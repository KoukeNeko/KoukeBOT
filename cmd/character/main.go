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
	"time"

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

	port := resolvePort()
	settingsStore := config.NewSettingsStore(settingsPath)
	settings := resolveSettings(settingsStore)

	personaService, err := loadPersonaService(personasDir)
	if err != nil {
		return fmt.Errorf("init persona service: %w", err)
	}
	runtime, err := buildRuntimeServices(ctx, settings, port, personaService)
	if err != nil {
		return err
	}

	discordBot := maybeStartDiscordBot(settings, personaService, runtime)
	oauthFlow := llm.NewGeminiOAuthFlow(
		oauthTokenPath,
		oauthPoolPath,
		"http://127.0.0.1:"+port+"/api/oauth/gemini/callback",
	)

	serverErrCh := startAPIServer(ctx, port, personaService, runtime.chatService, settingsStore, runtime.providerManager, discordBot, oauthFlow, runtime.convStore, runtime.imageStore)

	logSystem.Logf("ready — API on :%s", port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		logSystem.Log("shutting down...")
		cancel()
		return nil
	case err := <-serverErrCh:
		cancel()
		return err
	}
}

type runtimeServices struct {
	providerManager *app.ProviderManager
	chatService     *app.ChatService
	convStore       store.ConversationRepository
	imageStore      *store.ImageStore
	discordRuntime  *discordbot.Runtime
}

func resolvePort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	return port
}

func loadPersonaService(baseDir string) (*app.PersonaService, error) {
	personaService, err := app.NewPersonaService(baseDir)
	if err != nil {
		return nil, err
	}
	logPersona.Logf("loaded %d persona(s)", len(personaService.GetAll()))
	for id, p := range personaService.GetAll() {
		logPersona.Logf("  %s [%s]", p.Config.Meta.Name, id)
	}
	return personaService, nil
}

func buildRuntimeServices(
	ctx context.Context,
	settings config.Settings,
	port string,
	personaService *app.PersonaService,
) (*runtimeServices, error) {
	providerManager := app.NewProviderManager(nil)
	model := settings.GeminiModel
	if model == "" {
		model = defaultModel
	}
	initProvider(ctx, settings, providerManager, model, port)

	convStore, err := store.NewFileStore("data/conversations")
	if err != nil {
		return nil, fmt.Errorf("init conversation store: %w", err)
	}
	logStore.Log("data/conversations")

	imageStore, err := store.NewImageStore("data/images")
	if err != nil {
		return nil, fmt.Errorf("init image store: %w", err)
	}
	logStore.Log("data/images")

	discordRuntime := discordbot.NewRuntime(discordbot.RuntimeOptions{
		MemberIntentEnabled:   settings.DiscordMemberIntentEnabled,
		PresenceIntentEnabled: settings.DiscordPresenceIntentEnabled,
		MessageScope:          settings.DiscordObserveMessageScope,
		MaxResults:            settings.DiscordObserveMaxResults,
	})
	fileToolPersonaID := selectFileToolPersonaID(personasDir, settings.DiscordPersonaID)
	tools := initializeTools(settings, fileToolPersonaID, discordRuntime)
	if len(tools) > 0 {
		providerManager.RegisterTools(tools)
	}

	var contextManager *app.ContextManager
	if settings.ContextFlushEnabled {
		contextManager = app.NewContextManager(settings.ContextFlushThreshold, settings.ContextWindowSize)
		logSystem.Logf("context flush enabled (threshold: %d, window: %d)",
			settings.ContextFlushThreshold, settings.ContextWindowSize)
	}

	assembler := app.NewContextAssembler()
	assembler.UpdatePromptGuardPolicy(app.PromptGuardPolicy{
		PersonaLockEveryTurns:     settings.PersonaLockEveryTurns,
		TripletOutputEnabled:      settings.TripletOutputEnabled,
		AnalysisVisibility:        settings.AnalysisVisibility,
		NegativeConstraintEnabled: settings.NegativeConstraintEnabled,
		NegativeConstraintDefault: settings.NegativeConstraintDefault,
	})
	chatService := app.NewChatService(providerManager, assembler, convStore, imageStore, 0, personaService.GetAll(), tools, contextManager)
	chatService.UpdateToolExecutionPolicy(app.ToolExecutionPolicy{
		MaxToolRounds:                  settings.MaxToolRounds,
		MaxWebSearchCallsPerTurn:       settings.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: settings.ForceFinalAnswerAfterToolLimit,
		DiscordShowInternalToolErrors:  settings.DiscordShowInternalToolErrors,
	})

	return &runtimeServices{
		providerManager: providerManager,
		chatService:     chatService,
		convStore:       convStore,
		imageStore:      imageStore,
		discordRuntime:  discordRuntime,
	}, nil
}

func maybeStartDiscordBot(settings config.Settings, personaService *app.PersonaService, runtime *runtimeServices) *discordbot.Bot {
	if settings.DiscordToken == "" {
		logDiscord.Warnf("DISCORD_TOKEN not set — skipped")
		return nil
	}

	bot, err := startDiscordBot(
		settings.DiscordToken,
		settings.DiscordUseReply,
		settings.DiscordStripLeadingBotMention,
		settings.DiscordPersonaID,
		settings.DiscordMemberIntentEnabled,
		settings.DiscordPresenceIntentEnabled,
		personaService,
		runtime.chatService,
		runtime.convStore,
		runtime.imageStore,
		runtime.providerManager,
		runtime.discordRuntime,
	)
	if err != nil {
		logDiscord.Warnf("failed to start: %v", err)
		logDiscord.Log("continuing without Discord — HTTP API still available")
		return nil
	}
	if settings.LogChannelID != "" {
		logger.SetDiscordOutput(bot, settings.LogChannelID)
		logSystem.Logf("stdout → Discord channel %s", settings.LogChannelID)
	}
	return bot
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
	pool.SetPoolStorePath(oauthPoolPath)

	// Configure auto retry count from settings
	if settings.AutoRetryCount > 0 {
		pool.SetAutoRetryCount(settings.AutoRetryCount)
	}
	pool.SetTransientRecoveryWait(
		settings.TransientRecoveryWaitEnabled,
		time.Duration(settings.TransientRecoveryWaitMaxSeconds)*time.Second,
	)

	for _, tokenData := range tokens {
		if err := llm.RefreshIfNeeded(ctx, tokenData, ""); err != nil {
			logLLM.Warnf("OAuth token refresh failed for %s: %v", tokenData.Email, err)
			continue
		}
		if tokenData.Email != "" {
			if err := llm.UpdateOAuthPoolToken(oauthPoolPath, tokenData.Email, tokenData); err != nil {
				logLLM.Warnf("failed to persist refreshed pool token for %s: %v", tokenData.Email, err)
			}
		}
		provider, err := llm.NewGeminiOAuthProvider(ctx, tokenData, model)
		if err != nil {
			logLLM.Warnf("failed to init OAuth provider for %s: %v", tokenData.Email, err)
			continue
		}
		pool.AddOrReplaceWithUsage(provider, tokenData.Email, tokenData.UsageStats)
	}

	if pool.Size() == 0 {
		return false
	}

	pm.Update(pool)
	logLLM.Logf("OAuth pool: %d account(s) (model: %s)", pool.Size(), model)
	return true
}

func startDiscordBot(
	token string,
	useReply bool,
	stripLeadingBotMentionPrefix bool,
	preferredPersonaID string,
	enableMemberIntent bool,
	enablePresenceIntent bool,
	personaService *app.PersonaService,
	chatService *app.ChatService,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
	providerManager *app.ProviderManager,
	discordRuntime *discordbot.Runtime,
) (*discordbot.Bot, error) {
	// Use the first loaded persona as the default
	var personaID string
	if preferredPersonaID != "" {
		if _, ok := personaService.Get(preferredPersonaID); ok {
			personaID = preferredPersonaID
		}
	}
	if personaID == "" {
		for id := range personaService.GetAll() {
			personaID = id
			break
		}
	}

	if personaID == "" {
		return nil, fmt.Errorf("no personas loaded for Discord bot")
	}

	bot, err := discordbot.NewBot(token, personaID, discordbot.NewBotOptions{
		EnableMemberIntent:   enableMemberIntent,
		EnablePresenceIntent: enablePresenceIntent,
	})
	if err != nil {
		return nil, err
	}

	// Register message handler (responds to @mentions and DMs)
	handler := discordhandler.NewHandler(bot, chatService, convStore, useReply, stripLeadingBotMentionPrefix)
	handler.Register()

	// Connection must be open before registering slash commands
	// (RegisterCommands needs session.State.User.ID)
	if err := bot.Start(); err != nil {
		return nil, err
	}
	if discordRuntime != nil {
		discordRuntime.BindSession(bot.Session())
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
	discordBot *discordbot.Bot,
	oauthFlow *llm.GeminiOAuthFlow,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
) <-chan error {
	router := api.NewRouter(personaService, chatService, settingsStore, providerManager, discordBot, oauthFlow, convStore, imageStore, ctx)
	handler := router.Handler()

	// Wrap with SPA static file serving if web/dist exists
	webDir := filepath.Join("web", "dist")
	if info, err := os.Stat(webDir); err == nil && info.IsDir() {
		handler = withSPA(webDir, handler)
		logHTTP.Logf("frontend: %s", webDir)
	}

	errCh := make(chan error, 1)
	go func() {
		logHTTP.Logf("listening on :%s", port)
		if err := http.ListenAndServe(":"+port, handler); err != nil && err != http.ErrServerClosed {
			logHTTP.Errorf("server error: %v", err)
			errCh <- err
		}
		close(errCh)
	}()
	return errCh
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
func initializeTools(settings config.Settings, personaID string, discordRuntime *discordbot.Runtime) []domain.Tool {
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

	// Discord observe tool (read-only; Discord-source only).
	if settings.DiscordObserveEnabled && discordRuntime != nil {
		discordRuntime.SetOptions(discordbot.RuntimeOptions{
			MemberIntentEnabled:   settings.DiscordMemberIntentEnabled,
			PresenceIntentEnabled: settings.DiscordPresenceIntentEnabled,
			MessageScope:          settings.DiscordObserveMessageScope,
			MaxResults:            settings.DiscordObserveMaxResults,
		})
		tools = append(tools, llm.NewDiscordObserveTool(discordRuntime))
		toolNames = append(toolNames, "discord_observe (read-only; current guild scope)")
	}

	// File Operations Tools (declarations only).
	// Actual file tool execution is resolved dynamically by request persona at runtime.
	declarationPersonaID := personaID
	if declarationPersonaID == "" {
		declarationPersonaID = "_template"
	}
	declarationDir := filepath.Join("configs/personas", declarationPersonaID)
	tools = append(tools,
		llm.NewFileReadTool(declarationDir),
		llm.NewFileWriteTool(declarationDir),
		llm.NewFileAppendTool(declarationDir),
	)
	toolNames = append(toolNames, "file_read, file_write, file_append (declarations only; runtime persona-bound execution enabled)")

	// Print all tools in compact format
	for _, name := range toolNames {
		logTools.Logf("%s", name)
	}
	logTools.Logf("total: %d tool(s) registered", len(tools))

	return tools
}

func selectFileToolPersonaID(baseDir, preferred string) string {
	if preferred != "" {
		path := filepath.Join(baseDir, preferred)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return preferred
		}
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		return entry.Name()
	}
	return ""
}
