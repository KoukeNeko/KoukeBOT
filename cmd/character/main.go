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

	// Init memory extraction provider (cheap model, no tools registered)
	var memoryExtractor *app.MemoryExtractor
	if settings.MemoryExtractionEnabled && settings.GeminiAPIKey != "" {
		extractionModel := settings.MemoryExtractionModel
		if extractionModel == "" {
			extractionModel = "gemini-2.0-flash"
		}
		extractionProvider, err := llm.NewGeminiAPIProvider(ctx, settings.GeminiAPIKey, extractionModel)
		if err != nil {
			logMemory.Warnf("failed to init extraction provider: %v — disabled", err)
		} else {
			memoryExtractor = app.NewMemoryExtractor(extractionProvider, personasDir)
			logMemory.Logf("enabled (model: %s)", extractionModel)
		}
	} else if !settings.MemoryExtractionEnabled {
		logMemory.Log("disabled")
	}

	// Init core services
	convStore, err := store.NewFileStore("data/conversations")
	if err != nil {
		return fmt.Errorf("init conversation store: %w", err)
	}
	logStore.Log("data/conversations")

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
	chatService := app.NewChatService(providerManager, assembler, convStore, personaService.GetAll(), tools, memoryExtractor)

	// Start Discord bot (optional — only if DISCORD_TOKEN is set)
	if settings.DiscordToken != "" {
		if err := startDiscordBot(settings.DiscordToken, settings.DiscordUseReply, personaService, chatService, convStore); err != nil {
			logDiscord.Warnf("failed to start: %v", err)
			logDiscord.Log("continuing without Discord — HTTP API still available")
		}
	} else {
		logDiscord.Warnf("DISCORD_TOKEN not set — skipped")
	}

	// Init OAuth flow for Gemini CLI auth
	oauthRedirectURL := "http://localhost:" + port + "/api/oauth/gemini/callback"
	oauthFlow := llm.NewGeminiOAuthFlow(oauthTokenPath, oauthRedirectURL)

	// Start HTTP API server
	startAPIServer(ctx, port, personaService, chatService, settingsStore, providerManager, oauthFlow, convStore)

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
		if initOAuthProvider(ctx, pm, model) {
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
		if initOAuthProvider(ctx, pm, model) {
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

func initOAuthProvider(ctx context.Context, pm *app.ProviderManager, model string) bool {
	oauthData, err := llm.LoadStoredToken(oauthTokenPath)
	if err != nil || oauthData == nil {
		return false
	}
	if err := llm.RefreshIfNeeded(ctx, oauthData, oauthTokenPath); err != nil {
		logLLM.Warnf("OAuth token refresh failed: %v", err)
	}
	provider, err := llm.NewGeminiOAuthProvider(ctx, oauthData, model)
	if err != nil {
		logLLM.Warnf("failed to init Gemini OAuth provider: %v", err)
		return false
	}
	pm.Update(provider)
	logLLM.Logf("%s (model: %s, project: %s)", provider.ModelInfo().ProviderName, model, oauthData.ProjectID)
	return true
}

func startDiscordBot(token string, useReply bool, personaService *app.PersonaService, chatService *app.ChatService, convStore store.ConversationRepository) error {
	// Use the first loaded persona as the default
	var personaID string
	for id := range personaService.GetAll() {
		personaID = id
		break
	}

	if personaID == "" {
		return fmt.Errorf("no personas loaded for Discord bot")
	}

	bot, err := discordbot.NewBot(token, personaID)
	if err != nil {
		return err
	}

	// Register message handler (responds to @mentions and DMs)
	handler := discordhandler.NewHandler(bot, chatService, useReply)
	handler.Register()

	// Connection must be open before registering slash commands
	// (RegisterCommands needs session.State.User.ID)
	if err := bot.Start(); err != nil {
		return err
	}

	// Register slash commands: /persona, /reset, /status, /reload
	slashCmds := discordhandler.NewSlashCommands(bot, personaService, chatService, convStore)
	if err := slashCmds.Register(); err != nil {
		return fmt.Errorf("register slash commands: %w", err)
	}

	logDiscord.Logf("serving persona: %s", personaID)
	return nil
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
) {
	router := api.NewRouter(personaService, chatService, settingsStore, providerManager, oauthFlow, convStore, ctx)
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
