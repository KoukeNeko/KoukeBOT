package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	"github.com/KoukeNeko/KoukeBOT/internal/interface/api"
	discordhandler "github.com/KoukeNeko/KoukeBOT/internal/interface/discord"
)

const (
	personasDir    = "configs/personas"
	settingsPath   = "data/settings.json"
	oauthTokenPath = "data/gemini_oauth.json"
	defaultModel   = "gemini-2.5-pro-preview-06-05"
	defaultPort    = "8080"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
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
	log.Printf("loaded %d persona(s)", len(personaService.GetAll()))

	for id, p := range personaService.GetAll() {
		log.Printf("  - %s (%s)", p.Config.Meta.Name, id)
	}

	// Init LLM provider (optional — can be configured later via Web UI)
	providerManager := app.NewProviderManager(nil)
	model := settings.GeminiModel
	if model == "" {
		model = defaultModel
	}

	if settings.GeminiAPIKey != "" {
		provider, err := llm.NewGeminiAPIProvider(ctx, settings.GeminiAPIKey, model)
		if err != nil {
			log.Printf("WARNING: failed to init Gemini API provider: %v", err)
			log.Println("  → configure a valid API key at http://localhost:" + port + "/settings")
		} else {
			providerManager.Update(provider)
			log.Printf("LLM provider: %s (model: %s)", provider.ModelInfo().ProviderName, model)
		}
	} else if oauthData, err := llm.LoadStoredToken(oauthTokenPath); err == nil && oauthData != nil {
		if err := llm.RefreshIfNeeded(ctx, oauthData, oauthTokenPath); err != nil {
			log.Printf("WARNING: OAuth token refresh failed: %v", err)
		}
		provider, err := llm.NewGeminiOAuthProvider(ctx, oauthData, model)
		if err != nil {
			log.Printf("WARNING: failed to init Gemini OAuth provider: %v", err)
		} else {
			providerManager.Update(provider)
			log.Printf("LLM provider: %s (model: %s, project: %s)", provider.ModelInfo().ProviderName, model, oauthData.ProjectID)
		}
	} else {
		log.Println("no LLM credentials — configure at http://localhost:" + port + "/settings")
	}

	// Init core services
	convStore := store.NewMemoryStore()
	assembler := app.NewContextAssembler()
	chatService := app.NewChatService(providerManager, assembler, convStore, personaService.GetAll())

	// Start Discord bot (optional — only if DISCORD_TOKEN is set)
	if settings.DiscordToken != "" {
		if err := startDiscordBot(settings.DiscordToken, personaService, chatService, convStore); err != nil {
			return fmt.Errorf("start discord bot: %w", err)
		}
	} else {
		log.Println("DISCORD_TOKEN not set — skipping Discord bot")
	}

	// Init OAuth flow for Gemini CLI auth
	oauthRedirectURL := "http://localhost:" + port + "/api/oauth/gemini/callback"
	oauthFlow := llm.NewGeminiOAuthFlow(oauthTokenPath, oauthRedirectURL)

	// Start HTTP API server
	startAPIServer(ctx, port, personaService, chatService, settingsStore, providerManager, oauthFlow)

	log.Printf("character bot ready — API on :%s", port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	cancel()
	return nil
}

// resolveSettings loads persisted settings and overlays with environment variables.
// Env vars take precedence over the JSON file.
func resolveSettings(settingsStore *config.SettingsStore) config.Settings {
	settings, err := settingsStore.Load()
	if err != nil {
		log.Printf("WARNING: failed to load settings: %v", err)
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

func startDiscordBot(token string, personaService *app.PersonaService, chatService *app.ChatService, convStore store.ConversationRepository) error {
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
	handler := discordhandler.NewHandler(bot, chatService)
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

	log.Printf("discord bot serving persona: %s", personaID)
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
) {
	router := api.NewRouter(personaService, chatService, settingsStore, providerManager, oauthFlow, ctx)
	handler := router.Handler()

	// Wrap with SPA static file serving if web/dist exists
	webDir := filepath.Join("web", "dist")
	if info, err := os.Stat(webDir); err == nil && info.IsDir() {
		handler = withSPA(webDir, handler)
		log.Printf("serving web frontend from %s", webDir)
	}

	go func() {
		addr := ":" + port
		log.Printf("HTTP listening on %s", addr)
		if err := http.ListenAndServe(addr, handler); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
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
