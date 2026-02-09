package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/search"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	"github.com/KoukeNeko/KoukeBOT/internal/interface/api"
	discordhandler "github.com/KoukeNeko/KoukeBOT/internal/interface/discord"
)

const (
	personasDir    = "configs/personas"
	settingsPath   = "data/settings.json"
	oauthTokenPath = "data/gemini_oauth.json"
	oauthPoolPath  = "data/oauth_pool.json"
	defaultModel   = "gemini-2.5-pro"
	defaultPort    = "8080"
)

// Module loggers (Docker Compose style)
var (
	logSystem  = logger.New("system", logger.White)
	logPersona = logger.New("persona", logger.Cyan)
	logLLM     = logger.New("llm", logger.Yellow)
	logStore   = logger.New("store", logger.Blue)
	logTools   = logger.New("tools", logger.Green)
	logDiscord = logger.New("discord", logger.Magenta)
	logHTTP    = logger.New("http", logger.Blue)
	logMemory  = logger.New("memory", logger.Green)
)

func main() {
	if err := run(); err != nil {
		logSystem.Errorf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Load persisted settings, then overlay with env vars
	settingsStore := config.NewSettingsStore(settingsPath)
	settings := resolveSettings(settingsStore)

	// Load personas
	personaService, err := app.NewPersonaService(personasDir)
	if err != nil {
		return fmt.Errorf("init persona service: %w", err)
	}
	logPersona.Logf("loaded %d persona(s)", len(personaService.GetAll()))
	for id, p := range personaService.GetAll() {
		logPersona.Logf("  %s [%s]", p.Config.Meta.Name, id)
	}

	// Init LLM provider (optional — can be configured later via Web UI)
	providerManager := app.NewProviderManager(nil)
	model := settings.GeminiModel
	if model == "" {
		model = defaultModel
	}

	initProvider(ctx, settings, providerManager, model, port)

	// Init memory extraction provider (dedicated model, separate from main conversation)
	var memoryExtractor *app.MemoryExtractor
	if settings.MemoryExtractionEnabled {
		extractionModel := settings.MemoryExtractionModel
		if extractionModel == "" {
			extractionModel = "gemini-2.0-flash"
		}
		if settings.GeminiAPIKey != "" {
			// Dedicated provider with API key
			extractionProvider, err := llm.NewGeminiAPIProvider(ctx, settings.GeminiAPIKey, extractionModel)
			if err != nil {
				logMemory.Warnf("API extraction provider failed: %v", err)
			} else {
				memoryExtractor = app.NewMemoryExtractor(extractionProvider, personasDir)
				logMemory.Logf("enabled (api-key, model: %s)", extractionModel)
			}
		}
		// Try OAuth pool if no extractor yet
		if memoryExtractor == nil {
			extractionProvider := initOAuthExtractionProvider(ctx, extractionModel)
			if extractionProvider != nil {
				memoryExtractor = app.NewMemoryExtractor(extractionProvider, personasDir)
				logMemory.Logf("enabled (oauth, model: %s)", extractionModel)
			}
		}
		// Final fallback: use main provider (same model as conversation)
		if memoryExtractor == nil && providerManager.IsConfigured() {
			memoryExtractor = app.NewMemoryExtractor(providerManager, personasDir)
			logMemory.Logf("enabled (main provider fallback)")
		}
		if memoryExtractor == nil {
			logMemory.Warnf("enabled but no provider available — disabled")
		}
	} else {
		logMemory.Log("disabled")
	}

	// Init core services
	convStore, err := store.NewFileStore("data/conversations")
	if err != nil {
		return fmt.Errorf("init conversation store: %w", err)
	}
	logStore.Log("data/conversations")

	imageStore, err := store.NewImageStore("data/images")
	if err != nil {
		return fmt.Errorf("init image store: %w", err)
	}
	logStore.Log("data/images")

	// Initialize tools (OpenClaw-style web search)
	// Get the first persona directory for file operations (single-persona mode)
	var firstPersonaDir string
	entries, _ := os.ReadDir(personasDir)
	for _, entry := range entries {
		if entry.IsDir() {
			firstPersonaDir = entry.Name()
			break
		}
	}

	tools := initializeTools(settings, firstPersonaDir)
	if len(tools) > 0 {
		providerManager.RegisterTools(tools)
	}

	assembler := app.NewContextAssembler()
	chatService := app.NewChatService(providerManager, assembler, convStore, imageStore, 0, personaService.GetAll(), tools, memoryExtractor)

	// Start Discord bot (optional — only if DISCORD_TOKEN is set)
	if settings.DiscordToken != "" {
		bot, err := startDiscordBot(settings.DiscordToken, settings.DiscordUseReply, personaService, chatService, convStore, imageStore, providerManager)
		if err != nil {
			logDiscord.Warnf("failed to start: %v", err)
			logDiscord.Log("continuing without Discord — HTTP API still available")
		} else {
			// Enable Discord logging if channel ID is configured
			if settings.LogChannelID != "" {
				logger.SetDiscordOutput(bot, settings.LogChannelID)
				logSystem.Logf("stdout → Discord channel %s", settings.LogChannelID)
			}
		}
	} else {
		logDiscord.Warnf("DISCORD_TOKEN not set — skipped")
	}

	// Init OAuth flow for Gemini CLI auth
	oauthRedirectURL := "http://localhost:" + port + "/api/oauth/gemini/callback"
	oauthFlow := llm.NewGeminiOAuthFlow(oauthTokenPath, oauthPoolPath, oauthRedirectURL)

	// Start HTTP API server
	startAPIServer(ctx, port, personaService, chatService, settingsStore, providerManager, oauthFlow, convStore, imageStore)

	logSystem.Logf("ready — API on :%s", port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logSystem.Log("shutting down...")
	cancel()
	return nil
}

// resolveSettings loads persisted settings and overlays with environment variables.
// Env vars take precedence over the JSON file.
func resolveSettings(settingsStore *config.SettingsStore) config.Settings {
	settings, err := settingsStore.Load()
	if err != nil {
		logSystem.Warnf("failed to load settings: %v", err)
	}

	if envKey := os.Getenv("GEMINI_API_KEY"); envKey != "" {
		settings.GeminiAPIKey = envKey
	}
	if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		settings.GeminiModel = envModel
	}
	if envToken := os.Getenv("DISCORD_TOKEN"); envToken != "" {
		settings.DiscordToken = envToken
	}

	// Default: enable memory extraction if not explicitly configured
	// (JSON zero value for bool is false, so first-run users get it enabled)
	if settings.GeminiAPIKey != "" && !settings.MemoryExtractionEnabled && settings.MemoryExtractionModel == "" {
		settings.MemoryExtractionEnabled = true
	}

	return settings
}

// initProvider selects and initializes the LLM provider based on settings.
func initProvider(ctx context.Context, settings config.Settings, pm *app.ProviderManager, model, port string) {
	switch settings.ProviderType {
	case config.ProviderOAuth:
		if initOAuthProvider(ctx, settings, pm, model) {
			return
		}
		logLLM.Warnf("OAuth selected but not available — configure at http://localhost:%s/settings", port)

	case config.ProviderAPIKey:
		if initAPIKeyProvider(ctx, settings.GeminiAPIKey, pm, model) {
			return
		}
		logLLM.Warnf("API key selected but invalid — configure at http://localhost:%s/settings", port)

	default:
		// Auto: API key first, then OAuth.
		if settings.GeminiAPIKey != "" && initAPIKeyProvider(ctx, settings.GeminiAPIKey, pm, model) {
			return
		}
		if initOAuthProvider(ctx, settings, pm, model) {
			return
		}
		logLLM.Warnf("no LLM credentials — configure at http://localhost:%s/settings", port)
	}
}

func initAPIKeyProvider(ctx context.Context, apiKey string, pm *app.ProviderManager, model string) bool {
	if apiKey == "" {
		return false
	}
	provider, err := llm.NewGeminiAPIProvider(ctx, apiKey, model)
	if err != nil {
		logLLM.Warnf("failed to init Gemini API provider: %v", err)
		return false
	}
	pm.Update(provider)
	logLLM.Logf("%s (model: %s)", provider.ModelInfo().ProviderName, model)
	return true
}

func initOAuthProvider(ctx context.Context, settings config.Settings, pm *app.ProviderManager, model string) bool {
	// Migrate single token to pool if pool doesn't exist yet
	llm.MigrateSingleTokenToPool(oauthTokenPath, oauthPoolPath)

	// Load pool of OAuth tokens
	tokens, err := llm.LoadOAuthPool(oauthPoolPath)
	if err != nil {
		logLLM.Warnf("failed to load OAuth pool: %v", err)
	}

	// Fall back to single token if pool is empty
	if len(tokens) == 0 {
		oauthData, err := llm.LoadStoredToken(oauthTokenPath)
		if err != nil || oauthData == nil {
			return false
		}
		tokens = []*llm.OAuthTokenData{oauthData}
	}

	pool := app.NewProviderPool()

	// Configure auto retry count from settings
	if settings.AutoRetryCount > 0 {
		pool.SetAutoRetryCount(settings.AutoRetryCount)
	}

	for _, tokenData := range tokens {
		if err := llm.RefreshIfNeeded(ctx, tokenData, oauthTokenPath); err != nil {
			logLLM.Warnf("OAuth token refresh failed for %s: %v", tokenData.Email, err)
			continue
		}
		provider, err := llm.NewGeminiOAuthProvider(ctx, tokenData, model)
		if err != nil {
			logLLM.Warnf("failed to init OAuth provider for %s: %v", tokenData.Email, err)
			continue
		}
		pool.Add(provider, tokenData.Email)
	}

	if pool.Size() == 0 {
		return false
	}

	pm.Update(pool)
	logLLM.Logf("OAuth pool: %d account(s) (model: %s)", pool.Size(), model)
	return true
}

// initOAuthExtractionProvider creates a single OAuth provider with the specified model
// for memory extraction. Uses the first available token from the pool.
func initOAuthExtractionProvider(ctx context.Context, model string) llm.Provider {
	llm.MigrateSingleTokenToPool(oauthTokenPath, oauthPoolPath)

	tokens, err := llm.LoadOAuthPool(oauthPoolPath)
	if err != nil || len(tokens) == 0 {
		oauthData, err := llm.LoadStoredToken(oauthTokenPath)
		if err != nil || oauthData == nil {
			return nil
		}
		tokens = []*llm.OAuthTokenData{oauthData}
	}

	// Use first available token
	for _, tokenData := range tokens {
		if err := llm.RefreshIfNeeded(ctx, tokenData, oauthTokenPath); err != nil {
			continue
		}
		provider, err := llm.NewGeminiOAuthProvider(ctx, tokenData, model)
		if err != nil {
			continue
		}
		return provider
	}
	return nil
}

func startDiscordBot(token string, useReply bool, personaService *app.PersonaService, chatService *app.ChatService, convStore store.ConversationRepository, imageStore *store.ImageStore, providerManager *app.ProviderManager) (*discordbot.Bot, error) {
	// Use the first loaded persona as the default
	var personaID string
	for id := range personaService.GetAll() {
		personaID = id
		break
	}

	if personaID == "" {
		return nil, fmt.Errorf("no personas loaded for Discord bot")
	}

	bot, err := discordbot.NewBot(token, personaID)
	if err != nil {
		return nil, err
	}

	// Register message handler (responds to @mentions and DMs)
	handler := discordhandler.NewHandler(bot, chatService, useReply)
	handler.Register()

	// Connection must be open before registering slash commands
	// (RegisterCommands needs session.State.User.ID)
	if err := bot.Start(); err != nil {
		return nil, err
	}

	// Register slash commands: /persona, /reset, /status, /reload
	slashCmds := discordhandler.NewSlashCommands(bot, personaService, chatService, convStore, imageStore, providerManager)
	if err := slashCmds.Register(); err != nil {
		return nil, fmt.Errorf("register slash commands: %w", err)
	}

	logDiscord.Logf("serving persona: %s", personaID)
	return bot, nil
}

func startAPIServer(
	ctx context.Context,
	port string,
	personaService *app.PersonaService,
	chatService *app.ChatService,
	settingsStore *config.SettingsStore,
	providerManager *app.ProviderManager,
	oauthFlow *llm.GeminiOAuthFlow,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
) {
	router := api.NewRouter(personaService, chatService, settingsStore, providerManager, oauthFlow, convStore, imageStore, ctx)
	handler := router.Handler()

	// Wrap with SPA static file serving if web/dist exists
	webDir := filepath.Join("web", "dist")
	if info, err := os.Stat(webDir); err == nil && info.IsDir() {
		handler = withSPA(webDir, handler)
		logHTTP.Logf("frontend: %s", webDir)
	}

	go func() {
		logHTTP.Logf("listening on :%s", port)
		if err := http.ListenAndServe(":"+port, handler); err != nil && err != http.ErrServerClosed {
			logHTTP.Errorf("server error: %v", err)
			os.Exit(1)
		}
	}()
}

// withSPA wraps an API handler with SPA static file serving.
// /api/* requests go to the API; everything else serves static files
// with index.html fallback for client-side routing.
func withSPA(webDir string, apiHandler http.Handler) http.Handler {
	fs := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}

		// Try serving a static file
		filePath := filepath.Join(webDir, r.URL.Path)
		if _, err := os.Stat(filePath); err == nil {
			fs.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for unmatched paths
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
}

// initializeTools creates and returns enabled tools based on settings
func initializeTools(settings config.Settings, personaID string) []domain.Tool {
	var tools []domain.Tool
	var toolNames []string

	// Web Search Tool
	if settings.WebSearchEnabled {
		router := search.NewProviderRouter(
			settings.BraveAPIKey,
			settings.PerplexityAPIKey,
			settings.SearchProvider,
		)
		if router.IsConfigured() {
			tools = append(tools, llm.NewWebSearchTool(router))
			toolNames = append(toolNames, fmt.Sprintf("web_search (provider: %s)", settings.SearchProvider))
		}
	}

	// Web Fetch Tool
	if settings.WebFetchEnabled {
		tools = append(tools, llm.NewWebFetchTool(search.NewWebFetcher()))
		toolNames = append(toolNames, "web_fetch")
	}

	// Session Status Tool
	timezone := settings.Timezone
	if timezone == "" {
		timezone = "auto"
	}
	timeFormat := settings.TimeFormat
	if timeFormat == "" {
		timeFormat = "auto"
	}
	tools = append(tools, llm.NewSessionStatusTool(timezone, timeFormat))
	toolNames = append(toolNames, fmt.Sprintf("session_status (tz: %s)", timezone))

	// File Operations Tools
	if personaID != "" {
		personaDir := filepath.Join("configs/personas", personaID)
		tools = append(tools,
			llm.NewFileReadTool(personaDir),
			llm.NewFileWriteTool(personaDir),
			llm.NewFileAppendTool(personaDir),
		)
		toolNames = append(toolNames, fmt.Sprintf("file_read, file_write, file_append (persona: %s)", personaID))
	}

	// Print all tools in compact format
	for _, name := range toolNames {
		logTools.Logf("%s", name)
	}
	logTools.Logf("total: %d tool(s) registered", len(tools))

	return tools
}
