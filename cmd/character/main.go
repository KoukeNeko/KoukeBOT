package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

const (
	personasDir  = "configs/personas"
	defaultModel = "gemini-2.5-pro-preview-06-05"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY environment variable is required")
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = defaultModel
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
	provider, err := llm.NewGeminiAPIProvider(ctx, apiKey, model)
	if err != nil {
		return fmt.Errorf("init gemini provider: %w", err)
	}
	log.Printf("LLM provider: %s (model: %s)", provider.ModelInfo().ProviderName, model)

	// Init services
	convStore := store.NewMemoryStore()
	assembler := app.NewContextAssembler()
	chatService := app.NewChatService(provider, assembler, convStore, personaService.GetAll())

	// TODO: Start Discord bot
	// TODO: Start HTTP API server
	_ = chatService

	log.Println("character bot ready — waiting for shutdown signal")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	cancel()
	return nil
}
