package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	"github.com/KoukeNeko/KoukeBOT/internal/interface/api"
	discordhandler "github.com/KoukeNeko/KoukeBOT/internal/interface/discord"
)

const (
	personasDir  = "configs/personas"
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

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY environment variable is required")
	}

	discordToken := os.Getenv("DISCORD_TOKEN")

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = defaultModel
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Load personas
	personaService, err := app.NewPersonaService(personasDir)
	if err != nil {
		return fmt.Errorf("init persona service: %w", err)
	}
	log.Printf("loaded %d persona(s)", len(personaService.GetAll()))

	for id, p := range personaService.GetAll() {
		log.Printf("  - %s (%s)", p.Config.Meta.Name, id)
	}

	// Init LLM provider
	provider, err := llm.NewGeminiAPIProvider(ctx, geminiKey, model)
	if err != nil {
		return fmt.Errorf("init gemini provider: %w", err)
	}
	log.Printf("LLM provider: %s (model: %s)", provider.ModelInfo().ProviderName, model)

	// Init core services
	convStore := store.NewMemoryStore()
	assembler := app.NewContextAssembler()
	chatService := app.NewChatService(provider, assembler, convStore, personaService.GetAll())

	// Start Discord bot (optional — only if DISCORD_TOKEN is set)
	if discordToken != "" {
		if err := startDiscordBot(discordToken, personaService, chatService); err != nil {
			return fmt.Errorf("start discord bot: %w", err)
		}
	} else {
		log.Println("DISCORD_TOKEN not set — skipping Discord bot")
	}

	// Start HTTP API server
	startAPIServer(port, personaService, chatService)

	log.Printf("character bot ready — API on :%s", port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	cancel()
	return nil
}

func startDiscordBot(token string, personaService *app.PersonaService, chatService *app.ChatService) error {
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

	handler := discordhandler.NewHandler(bot, chatService)
	handler.Register()

	if err := bot.Start(); err != nil {
		return err
	}

	log.Printf("discord bot serving persona: %s", personaID)
	return nil
}

func startAPIServer(port string, personaService *app.PersonaService, chatService *app.ChatService) {
	router := api.NewRouter(personaService, chatService)

	go func() {
		addr := ":" + port
		log.Printf("HTTP API listening on %s", addr)
		if err := http.ListenAndServe(addr, router.Handler()); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()
}
