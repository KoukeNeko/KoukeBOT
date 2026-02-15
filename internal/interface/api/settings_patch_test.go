package api

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

func TestApplySettingsPatchKeepsMaskedSecretAndAppliesPointers(t *testing.T) {
	h := &Handlers{}
	existing := config.Settings{
		GeminiAPIKey:                  "real-key",
		WebSearchEnabled:              true,
		DiscordStripLeadingBotMention: false,
		ContextFlushThreshold:         4000,
		MemoryRecentRounds:            config.DefaultMemoryRecentRounds,
		ScheduledMonitorMaxPerUser:    config.DefaultScheduledMonitorMaxPerUser,
	}
	webSearchEnabled := false
	stripLeadingBotMention := true
	contextFlushThreshold := 0
	memoryRecentRounds := config.MaxMemoryRecentRounds
	monitorMaxPerUser := 7

	err := h.applySettingsPatch(&existing, SettingsUpdateRequest{
		GeminiAPIKey:                  "***...abcd",
		WebSearchEnabled:              &webSearchEnabled,
		DiscordStripLeadingBotMention: &stripLeadingBotMention,
		ContextFlushThreshold:         &contextFlushThreshold,
		MemoryRecentRounds:            &memoryRecentRounds,
		ScheduledMonitorMaxPerUser:    &monitorMaxPerUser,
	})
	if err != nil {
		t.Fatalf("applySettingsPatch returned error: %v", err)
	}
	if existing.GeminiAPIKey != "real-key" {
		t.Fatalf("gemini api key changed unexpectedly: %q", existing.GeminiAPIKey)
	}
	if existing.WebSearchEnabled {
		t.Fatalf("web_search_enabled should be false")
	}
	if !existing.DiscordStripLeadingBotMention {
		t.Fatalf("discord_strip_leading_bot_mention should be true")
	}
	if existing.ContextFlushThreshold != 0 {
		t.Fatalf("context_flush_threshold = %d, want 0", existing.ContextFlushThreshold)
	}
	if existing.MemoryRecentRounds != config.MaxMemoryRecentRounds {
		t.Fatalf("memory_recent_rounds = %d, want %d", existing.MemoryRecentRounds, config.MaxMemoryRecentRounds)
	}
	if existing.ScheduledMonitorMaxPerUser != monitorMaxPerUser {
		t.Fatalf("scheduled_monitor_max_per_user = %d, want %d", existing.ScheduledMonitorMaxPerUser, monitorMaxPerUser)
	}
}

func TestApplySettingsPatchRejectsInvalidPersonaID(t *testing.T) {
	personaService, err := app.NewPersonaService(testPersonasPath())
	if err != nil {
		t.Fatalf("load persona service: %v", err)
	}
	h := &Handlers{personaService: personaService}

	existing := config.Settings{}
	err = h.applySettingsPatch(&existing, SettingsUpdateRequest{DiscordPersonaID: "not-exists"})
	if err == nil {
		t.Fatalf("expected invalid persona id error")
	}
}

func TestSaveAndHotSwapSettingsReturnsWarningWithoutBlockingSave(t *testing.T) {
	tempDir := t.TempDir()
	settingsPath := filepath.Join(tempDir, "settings.json")
	personaService, err := app.NewPersonaService(testPersonasPath())
	if err != nil {
		t.Fatalf("load persona service: %v", err)
	}

	providerManager := app.NewProviderManager(nil)
	chatService := app.NewChatService(
		providerManager,
		app.NewContextAssembler(),
		store.NewMemoryStore(),
		nil,
		0,
		personaService.GetAll(),
		nil,
		nil,
	)

	h := &Handlers{
		personaService:  personaService,
		chatService:     chatService,
		settingsStore:   config.NewSettingsStore(settingsPath),
		providerManager: providerManager,
		oauthFlow: llm.NewGeminiOAuthFlow(
			filepath.Join(tempDir, "oauth.json"),
			filepath.Join(tempDir, "pool.json"),
			"http://127.0.0.1:8080/api/oauth/gemini/callback",
		),
		convStore: store.NewMemoryStore(),
		ctx:       context.Background(),
	}

	response, err := h.saveAndHotSwapSettings(config.Settings{
		ProviderType: config.ProviderOAuth,
		GeminiModel:  "gemini-2.5-pro",
	})
	if err != nil {
		t.Fatalf("saveAndHotSwapSettings returned error: %v", err)
	}
	if response["status"] != "saved" {
		t.Fatalf("status = %q, want saved", response["status"])
	}
	if response["warning"] == "" {
		t.Fatalf("expected warning when provider activation fails")
	}

	saved, err := h.settingsStore.Load()
	if err != nil {
		t.Fatalf("load saved settings: %v", err)
	}
	if saved.ProviderType != config.ProviderOAuth {
		t.Fatalf("saved provider type = %q, want oauth", saved.ProviderType)
	}
}

func testPersonasPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "configs", "personas"))
}
