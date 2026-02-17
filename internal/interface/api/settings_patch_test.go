package api

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

func TestApplySettingsPatchKeepsMaskedSecretAndAppliesPointers(t *testing.T) {
	h := &Handlers{}
	existing := config.Settings{
		GeminiAPIKey:                  "real-key",
		WebSearchEnabled:              true,
		WebToolMode:                   config.WebToolModeClassic,
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
	toolGuardEnabled := true

	err := h.applySettingsPatch(&existing, SettingsUpdateRequest{
		GeminiAPIKey:                  "***...abcd",
		WebSearchEnabled:              &webSearchEnabled,
		WebToolMode:                   config.WebToolModePlaywrightMCP,
		DiscordStripLeadingBotMention: &stripLeadingBotMention,
		ContextFlushThreshold:         &contextFlushThreshold,
		MemoryRecentRounds:            &memoryRecentRounds,
		ScheduledMonitorMaxPerUser:    &monitorMaxPerUser,
		ToolGuardEnabled:              &toolGuardEnabled,
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
	if existing.WebToolMode != config.WebToolModePlaywrightMCP {
		t.Fatalf("web_tool_mode = %q, want %q", existing.WebToolMode, config.WebToolModePlaywrightMCP)
	}
	if existing.MemoryRecentRounds != config.MaxMemoryRecentRounds {
		t.Fatalf("memory_recent_rounds = %d, want %d", existing.MemoryRecentRounds, config.MaxMemoryRecentRounds)
	}
	if existing.ScheduledMonitorMaxPerUser != monitorMaxPerUser {
		t.Fatalf("scheduled_monitor_max_per_user = %d, want %d", existing.ScheduledMonitorMaxPerUser, monitorMaxPerUser)
	}
	if !existing.ToolGuardEnabled {
		t.Fatalf("tool_guard_enabled should be true")
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

func TestApplySettingsPatchKeepsMaskedMCPSecrets(t *testing.T) {
	h := &Handlers{}
	existing := config.Settings{
		MCP: config.MCPSettings{
			Enabled:                 true,
			DiscoveryTimeoutSeconds: 10,
			ToolCallTimeoutSeconds:  20,
			Servers: []config.MCPServerSetting{
				{
					ID:            "remote_1",
					Name:          "Remote 1",
					Enabled:       true,
					Transport:     "http",
					URL:           "https://example.com/mcp",
					Headers:       map[string]string{"Authorization": "Bearer old-token"},
					Env:           map[string]string{"API_KEY": "old-key"},
					ToolAllowlist: []string{"search"},
				},
			},
		},
	}

	err := h.applySettingsPatch(&existing, SettingsUpdateRequest{
		MCP: &MCPSettingsUpdateRequest{
			Enabled:                 true,
			DiscoveryTimeoutSeconds: 10,
			ToolCallTimeoutSeconds:  20,
			Servers: []MCPServerUpdateRequest{
				{
					ID:            "remote_1",
					Name:          "Remote 1",
					Enabled:       true,
					Transport:     "http",
					URL:           "https://example.com/mcp",
					Headers:       map[string]string{"Authorization": "***...oken"},
					Env:           map[string]string{"API_KEY": "***...-key"},
					ToolAllowlist: []string{"search"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("applySettingsPatch returned error: %v", err)
	}
	if got := existing.MCP.Servers[0].Headers["Authorization"]; got != "Bearer old-token" {
		t.Fatalf("authorization header = %q, want preserved old token", got)
	}
	if got := existing.MCP.Servers[0].Env["API_KEY"]; got != "old-key" {
		t.Fatalf("env api key = %q, want preserved old key", got)
	}
}

func TestSaveAndHotSwapSettingsRejectsProviderRelatedChangeWhenProviderValidationFails(t *testing.T) {
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

	if err := h.settingsStore.Save(config.Settings{
		ProviderType: config.ProviderAPIKey,
		GeminiAPIKey: "existing-api-key",
		GeminiModel:  "gemini-2.5-pro",
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	_, err = h.saveAndHotSwapSettings(config.Settings{
		ProviderType: config.ProviderOAuth,
		GeminiModel:  "gemini-2.5-pro",
	})
	if err == nil {
		t.Fatalf("expected provider validation error")
	}
	if !isSettingsClientError(err) {
		t.Fatalf("expected settings client error, got: %v", err)
	}

	saved, err := h.settingsStore.Load()
	if err != nil {
		t.Fatalf("load saved settings: %v", err)
	}
	if saved.ProviderType != config.ProviderAPIKey {
		t.Fatalf("saved provider type = %q, want %q", saved.ProviderType, config.ProviderAPIKey)
	}
}

func TestSaveAndHotSwapSettingsAllowsNonProviderChangeWhenProviderIsUnhealthy(t *testing.T) {
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

	if err := h.settingsStore.Save(config.Settings{
		ProviderType: config.ProviderOAuth,
		GeminiModel:  "gemini-2.5-pro",
		Timezone:     "UTC",
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	response, err := h.saveAndHotSwapSettings(config.Settings{
		ProviderType: config.ProviderOAuth,
		GeminiModel:  "gemini-2.5-pro",
		Timezone:     "Asia/Tokyo",
	})
	if err != nil {
		t.Fatalf("saveAndHotSwapSettings returned error: %v", err)
	}
	if response["status"] != "saved" {
		t.Fatalf("status = %q, want saved", response["status"])
	}
	if response["warning"] != "" {
		t.Fatalf("unexpected warning: %q", response["warning"])
	}

	saved, err := h.settingsStore.Load()
	if err != nil {
		t.Fatalf("load saved settings: %v", err)
	}
	if saved.ProviderType != config.ProviderOAuth {
		t.Fatalf("saved provider type = %q, want %q", saved.ProviderType, config.ProviderOAuth)
	}
	if saved.Timezone != "Asia/Tokyo" {
		t.Fatalf("saved timezone = %q, want Asia/Tokyo", saved.Timezone)
	}
}

func TestSaveAndHotSwapSettingsUpdatesAssemblerGlobalTimezone(t *testing.T) {
	tempDir := t.TempDir()
	settingsPath := filepath.Join(tempDir, "settings.json")
	personaService, err := app.NewPersonaService(testPersonasPath())
	if err != nil {
		t.Fatalf("load persona service: %v", err)
	}

	providerManager := app.NewProviderManager(nil)
	assembler := app.NewContextAssembler()
	personaID := "persona-no-timezone"
	personas := map[string]*domain.PersonaModule{
		personaID: {
			Config: domain.PersonaConfig{
				Meta: domain.PersonaMeta{
					ID:   personaID,
					Name: "Persona No TZ",
				},
				SystemTemplate: "Current Time: {{.Time}}",
				Variables:      map[string]any{},
			},
		},
	}
	chatService := app.NewChatService(
		providerManager,
		assembler,
		store.NewMemoryStore(),
		nil,
		0,
		personas,
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
		Timezone: "Asia/Tokyo",
	})
	if err != nil {
		t.Fatalf("saveAndHotSwapSettings returned error: %v", err)
	}
	if response["status"] != "saved" {
		t.Fatalf("status = %q, want saved", response["status"])
	}

	assembled, err := assembler.Assemble(personas[personaID], &domain.Conversation{}, llm.ModelCapabilities{
		SupportsThinking: true,
		ContextWindow:    8000,
	})
	if err != nil {
		t.Fatalf("assembler.Assemble returned error: %v", err)
	}
	if !strings.Contains(assembled.SystemPrompt, "Time zone: Asia/Tokyo") {
		t.Fatalf("expected updated global timezone in system prompt, got:\n%s", assembled.SystemPrompt)
	}
}

func TestHasProviderRelatedSettingsChange(t *testing.T) {
	base := config.Settings{
		ProviderType:         config.ProviderAPIKey,
		GeminiModel:          "models/gemini-2.5-pro",
		GeminiAPIKey:         "api-key-a",
		FallbackModelEnabled: true,
		FallbackModel:        "models/gemini-2.0-flash",
		DiscordUseReply:      false,
	}

	tests := []struct {
		name string
		next config.Settings
		want bool
	}{
		{
			name: "non-provider field only",
			next: config.Settings{
				ProviderType:         config.ProviderAPIKey,
				GeminiModel:          "gemini-2.5-pro",
				GeminiAPIKey:         "api-key-a",
				FallbackModelEnabled: true,
				FallbackModel:        "gemini-2.0-flash",
				DiscordUseReply:      true,
			},
			want: false,
		},
		{
			name: "provider type changed",
			next: config.Settings{
				ProviderType:         config.ProviderOAuth,
				GeminiModel:          "gemini-2.5-pro",
				GeminiAPIKey:         "api-key-a",
				FallbackModelEnabled: true,
				FallbackModel:        "gemini-2.0-flash",
			},
			want: true,
		},
		{
			name: "model changed",
			next: config.Settings{
				ProviderType:         config.ProviderAPIKey,
				GeminiModel:          "gemini-2.5-flash",
				GeminiAPIKey:         "api-key-a",
				FallbackModelEnabled: true,
				FallbackModel:        "gemini-2.0-flash",
			},
			want: true,
		},
		{
			name: "api key changed",
			next: config.Settings{
				ProviderType:         config.ProviderAPIKey,
				GeminiModel:          "gemini-2.5-pro",
				GeminiAPIKey:         "api-key-b",
				FallbackModelEnabled: true,
				FallbackModel:        "gemini-2.0-flash",
			},
			want: true,
		},
		{
			name: "fallback switch changed",
			next: config.Settings{
				ProviderType:         config.ProviderAPIKey,
				GeminiModel:          "gemini-2.5-pro",
				GeminiAPIKey:         "api-key-a",
				FallbackModelEnabled: false,
				FallbackModel:        "gemini-2.0-flash",
			},
			want: true,
		},
		{
			name: "fallback model changed",
			next: config.Settings{
				ProviderType:         config.ProviderAPIKey,
				GeminiModel:          "gemini-2.5-pro",
				GeminiAPIKey:         "api-key-a",
				FallbackModelEnabled: true,
				FallbackModel:        "gemini-1.5-pro",
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hasProviderRelatedSettingsChange(base, tc.next)
			if got != tc.want {
				t.Fatalf("hasProviderRelatedSettingsChange() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldRunStrictOAuthProbe(t *testing.T) {
	tests := []struct {
		name     string
		settings config.Settings
		want     bool
	}{
		{
			name: "oauth provider",
			settings: config.Settings{
				ProviderType: config.ProviderOAuth,
			},
			want: true,
		},
		{
			name: "api key provider",
			settings: config.Settings{
				ProviderType: config.ProviderAPIKey,
				GeminiAPIKey: "key",
			},
			want: false,
		},
		{
			name: "auto provider with api key",
			settings: config.Settings{
				ProviderType: "",
				GeminiAPIKey: "key",
			},
			want: false,
		},
		{
			name: "auto provider without api key",
			settings: config.Settings{
				ProviderType: "",
				GeminiAPIKey: "",
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunStrictOAuthProbe(tc.settings)
			if got != tc.want {
				t.Fatalf("shouldRunStrictOAuthProbe() = %v, want %v", got, tc.want)
			}
		})
	}
}

func testPersonasPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "configs", "personas"))
}
