package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	personasDir  = "configs/personas"
	settingsPath = "data/settings.json"
	defaultModel = "gemini-2.5-pro-preview-06-05"
	defaultPort  = "8080"
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

	if settings.GeminiAPIKey != "" {
		model := settings.GeminiModel
		if model == "" {
			model = defaultModel
		}

		provider, err := llm.NewGeminiAPIProvider(ctx, settings.GeminiAPIKey, model)
		if err != nil {
			log.Printf("WARNING: failed to init Gemini provider: %v", err)
			log.Println("  → configure a valid API key at http://localhost:" + port + "/settings")
		} else {
			providerManager.Update(provider)
			log.Printf("LLM provider: %s (model: %s)", provider.ModelInfo().ProviderName, model)
		}
	} else {
		log.Println("GEMINI_API_KEY not set — configure it at http://localhost:" + port + "/settings")
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

	// Start HTTP API server
	startAPIServer(ctx, port, personaService, chatService, settingsStore, providerManager)

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
) {
	// Serve React frontend from web/dist if it exists
	webDir := filepath.Join("web", "dist")
	router := api.NewRouter(personaService, chatService, settingsStore, providerManager, ctx)
	mux := http.NewServeMux()

	// API routes first
	mux.Handle("/api/", router.Handler())

	// Static files for the web frontend
	if info, err := os.Stat(webDir); err == nil && info.IsDir() {
		fs := http.FileServer(http.Dir(webDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// SPA fallback: serve index.html for non-file paths
			path := filepath.Join(webDir, r.URL.Path)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
				return
			}
			fs.ServeHTTP(w, r)
		})
		log.Printf("serving web frontend from %s", webDir)
	}

	go func() {
		addr := ":" + port
		log.Printf("HTTP listening on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()
}
