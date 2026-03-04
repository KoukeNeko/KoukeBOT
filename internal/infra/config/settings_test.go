package config

import (
	"encoding/json"
	"testing"
)

func TestNormalizeProviderType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: ProviderAPIKey, want: ProviderAPIKey},
		{in: ProviderOAuth, want: ProviderOAuth},
		{in: "cli", want: ProviderOAuth},
		{in: "legacy", want: ProviderOAuth},
	}

	for _, tt := range tests {
		if got := normalizeProviderType(tt.in); got != tt.want {
			t.Fatalf("normalizeProviderType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeWebToolMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: WebToolModeClassic},
		{in: WebToolModeClassic, want: WebToolModeClassic},
		{in: WebToolModePlaywrightMCP, want: WebToolModePlaywrightMCP},
		{in: "unknown", want: WebToolModeClassic},
	}

	for _, tt := range tests {
		if got := normalizeWebToolMode(tt.in); got != tt.want {
			t.Fatalf("normalizeWebToolMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeSettingsAppliesPromptGuardDefaultsForLegacyFile(t *testing.T) {
	var s Settings
	raw := map[string]json.RawMessage{}
	got := normalizeSettings(s, raw)

	if got.PersonaLockEveryTurns != DefaultPersonaLockEveryTurns {
		t.Fatalf("PersonaLockEveryTurns = %d, want %d", got.PersonaLockEveryTurns, DefaultPersonaLockEveryTurns)
	}
	if !got.TripletOutputEnabled {
		t.Fatalf("TripletOutputEnabled should default to true for legacy settings")
	}
	if !got.NegativeConstraintEnabled {
		t.Fatalf("NegativeConstraintEnabled should default to true for legacy settings")
	}
	if got.AnalysisVisibility != DefaultAnalysisVisibility {
		t.Fatalf("AnalysisVisibility = %q, want %q", got.AnalysisVisibility, DefaultAnalysisVisibility)
	}
	if got.NegativeConstraintDefault != DefaultNegativeConstraintPrompt {
		t.Fatalf("NegativeConstraintDefault = %q, want default", got.NegativeConstraintDefault)
	}
	if got.DiscordObserveMaxResults != DefaultDiscordObserveMaxResults {
		t.Fatalf("DiscordObserveMaxResults = %d, want %d", got.DiscordObserveMaxResults, DefaultDiscordObserveMaxResults)
	}
	if got.DiscordObserveMessageScope != DefaultDiscordObserveMessageScope {
		t.Fatalf("DiscordObserveMessageScope = %q, want %q", got.DiscordObserveMessageScope, DefaultDiscordObserveMessageScope)
	}
	if got.MaxToolRounds != DefaultMaxToolRounds {
		t.Fatalf("MaxToolRounds = %d, want %d", got.MaxToolRounds, DefaultMaxToolRounds)
	}
	if got.ToolGuardEnabled != DefaultToolGuardEnabled {
		t.Fatalf("ToolGuardEnabled = %v, want %v", got.ToolGuardEnabled, DefaultToolGuardEnabled)
	}
	if got.MaxWebSearchCallsPerTurn != DefaultMaxWebSearchCallsPerTurn {
		t.Fatalf("MaxWebSearchCallsPerTurn = %d, want %d", got.MaxWebSearchCallsPerTurn, DefaultMaxWebSearchCallsPerTurn)
	}
	if !got.ForceFinalAnswerAfterToolLimit {
		t.Fatalf("ForceFinalAnswerAfterToolLimit should default to true for legacy settings")
	}
	if !got.TransientRecoveryWaitEnabled {
		t.Fatalf("TransientRecoveryWaitEnabled should default to true for legacy settings")
	}
	if got.TransientRecoveryWaitMaxSeconds != DefaultTransientRecoveryWaitMax {
		t.Fatalf("TransientRecoveryWaitMaxSeconds = %d, want %d", got.TransientRecoveryWaitMaxSeconds, DefaultTransientRecoveryWaitMax)
	}
	if !got.DiscordShowInternalToolErrors {
		t.Fatalf("DiscordShowInternalToolErrors should default to true for legacy settings")
	}
	if got.DiscordStripLeadingBotMention {
		t.Fatalf("DiscordStripLeadingBotMention should default to false (preserve mention text)")
	}
	if got.MemoryRecentRounds != DefaultMemoryRecentRounds {
		t.Fatalf("MemoryRecentRounds = %d, want %d", got.MemoryRecentRounds, DefaultMemoryRecentRounds)
	}
	if got.ScheduledMonitorMaxPerUser != DefaultScheduledMonitorMaxPerUser {
		t.Fatalf("ScheduledMonitorMaxPerUser = %d, want %d", got.ScheduledMonitorMaxPerUser, DefaultScheduledMonitorMaxPerUser)
	}
}

func TestNormalizeSettingsKeepsExplicitFalseValues(t *testing.T) {
	explicitFalse := json.RawMessage("false")
	debugValue := json.RawMessage(`"debug"`)
	constraintValue := json.RawMessage(`"keep this"`)
	intVal := json.RawMessage(`3`)
	waitVal := json.RawMessage(`5`)
	raw := map[string]json.RawMessage{
		"triplet_output_enabled":              explicitFalse,
		"negative_constraint_enabled":         explicitFalse,
		"analysis_visibility":                 debugValue,
		"negative_constraint_default":         constraintValue,
		"tool_guard_enabled":                  explicitFalse,
		"force_final_answer_after_tool_limit": explicitFalse,
		"transient_recovery_wait_enabled":     explicitFalse,
		"discord_show_internal_tool_errors":   explicitFalse,
		"discord_strip_leading_bot_mention":   json.RawMessage("true"),
		"max_tool_rounds":                     intVal,
		"max_web_search_calls_per_turn":       intVal,
		"transient_recovery_wait_max_seconds": waitVal,
	}
	in := Settings{
		PersonaLockEveryTurns:           6,
		TripletOutputEnabled:            false,
		NegativeConstraintEnabled:       false,
		AnalysisVisibility:              "debug",
		NegativeConstraintDefault:       "keep this",
		ToolGuardEnabled:                false,
		ForceFinalAnswerAfterToolLimit:  false,
		TransientRecoveryWaitEnabled:    false,
		DiscordShowInternalToolErrors:   false,
		DiscordStripLeadingBotMention:   true,
		MaxToolRounds:                   3,
		MaxWebSearchCallsPerTurn:        3,
		TransientRecoveryWaitMaxSeconds: 5,
	}
	got := normalizeSettings(in, raw)

	if got.TripletOutputEnabled {
		t.Fatalf("TripletOutputEnabled should keep explicit false")
	}
	if got.NegativeConstraintEnabled {
		t.Fatalf("NegativeConstraintEnabled should keep explicit false")
	}
	if got.AnalysisVisibility != "debug" {
		t.Fatalf("AnalysisVisibility = %q, want debug", got.AnalysisVisibility)
	}
	if got.NegativeConstraintDefault != "keep this" {
		t.Fatalf("NegativeConstraintDefault changed unexpectedly: %q", got.NegativeConstraintDefault)
	}
	if got.ToolGuardEnabled {
		t.Fatalf("ToolGuardEnabled should keep explicit false")
	}
	if got.ForceFinalAnswerAfterToolLimit {
		t.Fatalf("ForceFinalAnswerAfterToolLimit should keep explicit false")
	}
	if got.TransientRecoveryWaitEnabled {
		t.Fatalf("TransientRecoveryWaitEnabled should keep explicit false")
	}
	if got.DiscordShowInternalToolErrors {
		t.Fatalf("DiscordShowInternalToolErrors should keep explicit false")
	}
	if !got.DiscordStripLeadingBotMention {
		t.Fatalf("DiscordStripLeadingBotMention should keep explicit true")
	}
}

func TestNormalizeSettingsDiscordObserveDefaultsOnSavePath(t *testing.T) {
	got := normalizeSettings(Settings{}, nil)
	if got.DiscordObserveMaxResults != DefaultDiscordObserveMaxResults {
		t.Fatalf("DiscordObserveMaxResults = %d, want %d", got.DiscordObserveMaxResults, DefaultDiscordObserveMaxResults)
	}
	if got.DiscordObserveMessageScope != DefaultDiscordObserveMessageScope {
		t.Fatalf("DiscordObserveMessageScope = %q, want %q", got.DiscordObserveMessageScope, DefaultDiscordObserveMessageScope)
	}
	if got.MemoryRecentRounds != DefaultMemoryRecentRounds {
		t.Fatalf("MemoryRecentRounds = %d, want %d", got.MemoryRecentRounds, DefaultMemoryRecentRounds)
	}
	if got.ScheduledMonitorMaxPerUser != DefaultScheduledMonitorMaxPerUser {
		t.Fatalf("ScheduledMonitorMaxPerUser = %d, want %d", got.ScheduledMonitorMaxPerUser, DefaultScheduledMonitorMaxPerUser)
	}
}

func TestNormalizeSettingsMemoryRecentRoundsBounds(t *testing.T) {
	gotLow := normalizeSettings(Settings{MemoryRecentRounds: MinMemoryRecentRounds - 1}, nil)
	if gotLow.MemoryRecentRounds != DefaultMemoryRecentRounds {
		t.Fatalf("MemoryRecentRounds low = %d, want %d", gotLow.MemoryRecentRounds, DefaultMemoryRecentRounds)
	}

	gotHigh := normalizeSettings(Settings{MemoryRecentRounds: MaxMemoryRecentRounds + 20}, nil)
	if gotHigh.MemoryRecentRounds != MaxMemoryRecentRounds {
		t.Fatalf("MemoryRecentRounds high = %d, want %d", gotHigh.MemoryRecentRounds, MaxMemoryRecentRounds)
	}

	gotMonitorLow := normalizeSettings(Settings{ScheduledMonitorMaxPerUser: 0}, nil)
	if gotMonitorLow.ScheduledMonitorMaxPerUser != DefaultScheduledMonitorMaxPerUser {
		t.Fatalf("ScheduledMonitorMaxPerUser low = %d, want %d", gotMonitorLow.ScheduledMonitorMaxPerUser, DefaultScheduledMonitorMaxPerUser)
	}

	gotMonitorHigh := normalizeSettings(Settings{ScheduledMonitorMaxPerUser: MaxScheduledMonitorMaxPerUser + 10}, nil)
	if gotMonitorHigh.ScheduledMonitorMaxPerUser != MaxScheduledMonitorMaxPerUser {
		t.Fatalf("ScheduledMonitorMaxPerUser high = %d, want %d", gotMonitorHigh.ScheduledMonitorMaxPerUser, MaxScheduledMonitorMaxPerUser)
	}
}

func TestNormalizeSettingsWebToolModeDefaults(t *testing.T) {
	got := normalizeSettings(Settings{}, map[string]json.RawMessage{})
	if got.WebToolMode != WebToolModeClassic {
		t.Fatalf("WebToolMode = %q, want %q", got.WebToolMode, WebToolModeClassic)
	}

	gotInvalid := normalizeSettings(Settings{WebToolMode: "bad"}, map[string]json.RawMessage{
		"web_tool_mode": json.RawMessage(`"bad"`),
	})
	if gotInvalid.WebToolMode != WebToolModeClassic {
		t.Fatalf("invalid WebToolMode normalized to %q, want %q", gotInvalid.WebToolMode, WebToolModeClassic)
	}
}

func TestNormalizeSettingsNormalizesLegacyClaudeModelAlias(t *testing.T) {
	in := Settings{
		ProviderType: ProviderOAuth,
		ProviderID:   "google-antigravity",
		ModelID:      "google-antigravity/claude-opus-4-6",
		GeminiModel:  "claude-opus-4-6",
	}
	raw := map[string]json.RawMessage{
		"provider_id":  json.RawMessage(`"google-antigravity"`),
		"model_id":     json.RawMessage(`"google-antigravity/claude-opus-4-6"`),
		"gemini_model": json.RawMessage(`"claude-opus-4-6"`),
	}

	got := normalizeSettings(in, raw)
	if got.GeminiModel != "claude-opus-4-6-thinking" {
		t.Fatalf("GeminiModel = %q, want %q", got.GeminiModel, "claude-opus-4-6-thinking")
	}
	if got.ModelID != "google-antigravity/claude-opus-4-6-thinking" {
		t.Fatalf("ModelID = %q, want %q", got.ModelID, "google-antigravity/claude-opus-4-6-thinking")
	}
}

func TestNormalizeSettingsSupportsAIStudioRPCProviderID(t *testing.T) {
	in := Settings{
		ProviderType: ProviderOAuth,
		ProviderID:   "aistudio",
		ModelID:      "aistudio-rpc/gemini-3-pro-preview",
		GeminiModel:  "gemini-3-pro-preview",
	}
	raw := map[string]json.RawMessage{
		"provider_id": json.RawMessage(`"aistudio"`),
		"model_id":    json.RawMessage(`"aistudio-rpc/gemini-3-pro-preview"`),
	}

	got := normalizeSettings(in, raw)
	if got.ProviderID != "aistudio-rpc" {
		t.Fatalf("ProviderID = %q, want %q", got.ProviderID, "aistudio-rpc")
	}
	if got.ProviderType != ProviderOAuth {
		t.Fatalf("ProviderType = %q, want %q", got.ProviderType, ProviderOAuth)
	}
	if got.ModelID != "aistudio-rpc/gemini-3-pro-preview" {
		t.Fatalf("ModelID = %q, want %q", got.ModelID, "aistudio-rpc/gemini-3-pro-preview")
	}
}

func TestNormalizeSettingsAllowsGeminiCLILocalMode(t *testing.T) {
	in := Settings{
		ProviderType: "",
		ProviderID:   "google-gemini-cli",
		ModelID:      "google-gemini-cli/gemini-3.1-pro-preview",
		GeminiModel:  "gemini-3.1-pro-preview",
	}
	raw := map[string]json.RawMessage{
		"provider_type": json.RawMessage(`""`),
		"provider_id":   json.RawMessage(`"google-gemini-cli"`),
		"model_id":      json.RawMessage(`"google-gemini-cli/gemini-3.1-pro-preview"`),
	}

	got := normalizeSettings(in, raw)
	if got.ProviderID != "google-gemini-cli" {
		t.Fatalf("ProviderID = %q, want %q", got.ProviderID, "google-gemini-cli")
	}
	if got.ProviderType != "" {
		t.Fatalf("ProviderType = %q, want empty for local CLI mode", got.ProviderType)
	}
	if got.ModelID != "google-gemini-cli/gemini-3.1-pro-preview" {
		t.Fatalf("ModelID = %q, want %q", got.ModelID, "google-gemini-cli/gemini-3.1-pro-preview")
	}
}

func TestNormalizeSettingsBuildsFallbackChainAndRemovesPrimaryOrDuplicates(t *testing.T) {
	in := Settings{
		ProviderType:             ProviderOAuth,
		ProviderID:               "google-gemini-cli",
		ModelID:                  "google-gemini-cli/gemini-3-pro-preview",
		GeminiModel:              "gemini-3-pro-preview",
		FallbackModelID:          "google-gemini-cli/gemini-3-pro-preview",
		FallbackModel:            "gemini-3-pro-preview",
		FallbackSecondModelID:    "google-gemini-cli/gemini-2.5-pro",
		FallbackSecondModel:      "gemini-2.5-pro",
		FallbackThirdModelID:     "google-gemini-cli/gemini-2.5-pro",
		FallbackThirdModel:       "gemini-2.5-pro",
		FallbackEnabled:          true,
		FallbackModelEnabled:     true,
		RequestQueueEnabled:      true,
		RequestQueueCapacity:     DefaultRequestQueueCapacity,
		RequestQueueTimeoutS:     DefaultRequestQueueTimeoutS,
		MemoryRecentRounds:       DefaultMemoryRecentRounds,
		MaxToolRounds:            DefaultMaxToolRounds,
		MaxWebSearchCallsPerTurn: DefaultMaxWebSearchCallsPerTurn,
	}

	got := normalizeSettings(in, nil)
	if got.FallbackModelID != "google-gemini-cli/gemini-2.5-pro" {
		t.Fatalf("FallbackModelID = %q, want %q", got.FallbackModelID, "google-gemini-cli/gemini-2.5-pro")
	}
	if got.FallbackModel != "gemini-2.5-pro" {
		t.Fatalf("FallbackModel = %q, want %q", got.FallbackModel, "gemini-2.5-pro")
	}
	if got.FallbackSecondModelID != "" {
		t.Fatalf("FallbackSecondModelID = %q, want empty", got.FallbackSecondModelID)
	}
	if got.FallbackSecondModel != "" {
		t.Fatalf("FallbackSecondModel = %q, want empty", got.FallbackSecondModel)
	}
	if got.FallbackThirdModelID != "" {
		t.Fatalf("FallbackThirdModelID = %q, want empty", got.FallbackThirdModelID)
	}
	if got.FallbackThirdModel != "" {
		t.Fatalf("FallbackThirdModel = %q, want empty", got.FallbackThirdModel)
	}
}

func TestResolveFallbackModelChainReturnsOrderedUniqueModels(t *testing.T) {
	settings := Settings{
		FallbackModelID:       "google-gemini-cli/gemini-3-flash",
		FallbackModel:         "gemini-3-flash",
		FallbackSecondModelID: "google-gemini-cli/gemini-2.0-flash",
		FallbackSecondModel:   "gemini-2.0-flash",
		FallbackThirdModelID:  "google-gemini-cli/gemini-2.5-pro",
		FallbackThirdModel:    "gemini-2.5-pro",
	}

	got := ResolveFallbackModelChain(settings, "google-gemini-cli", "gemini-3-pro-preview")
	if len(got) != 3 {
		t.Fatalf("len(chain) = %d, want 3", len(got))
	}
	if got[0] != "gemini-3-flash-preview" {
		t.Fatalf("chain[0] = %q, want %q", got[0], "gemini-3-flash-preview")
	}
	if got[1] != "gemini-2.0-flash" {
		t.Fatalf("chain[1] = %q, want %q", got[1], "gemini-2.0-flash")
	}
	if got[2] != "gemini-2.5-pro" {
		t.Fatalf("chain[2] = %q, want %q", got[2], "gemini-2.5-pro")
	}

	ids := ResolveFallbackModelIDChain(settings, "google-gemini-cli", "gemini-3-pro-preview")
	if len(ids) != 3 {
		t.Fatalf("len(id_chain) = %d, want 3", len(ids))
	}
	if ids[0] != "google-gemini-cli/gemini-3-flash-preview" {
		t.Fatalf("id_chain[0] = %q, want %q", ids[0], "google-gemini-cli/gemini-3-flash-preview")
	}
	if ids[1] != "google-gemini-cli/gemini-2.0-flash" {
		t.Fatalf("id_chain[1] = %q, want %q", ids[1], "google-gemini-cli/gemini-2.0-flash")
	}
	if ids[2] != "google-gemini-cli/gemini-2.5-pro" {
		t.Fatalf("id_chain[2] = %q, want %q", ids[2], "google-gemini-cli/gemini-2.5-pro")
	}
}

func TestNormalizeSettingsSupportsAnthropicAPIKeyProvider(t *testing.T) {
	in := Settings{
		ProviderType:    ProviderAPIKey,
		ProviderID:      "anthropic-api-key",
		ModelID:         "anthropic-api-key/claude-opus-4-6",
		GeminiModel:     "claude-opus-4-6",
		AnthropicAPIKey: "sk-ant-api-1",
	}
	raw := map[string]json.RawMessage{
		"provider_type":     json.RawMessage(`"api-key"`),
		"provider_id":       json.RawMessage(`"anthropic-api-key"`),
		"model_id":          json.RawMessage(`"anthropic-api-key/claude-opus-4-6"`),
		"anthropic_api_key": json.RawMessage(`"sk-ant-api-1"`),
		"gemini_model":      json.RawMessage(`"claude-opus-4-6"`),
	}

	got := normalizeSettings(in, raw)
	if got.ProviderID != "anthropic-api-key" {
		t.Fatalf("ProviderID = %q, want %q", got.ProviderID, "anthropic-api-key")
	}
	if got.ProviderType != ProviderAPIKey {
		t.Fatalf("ProviderType = %q, want %q", got.ProviderType, ProviderAPIKey)
	}
	if got.GeminiModel != "claude-opus-4-6" {
		t.Fatalf("GeminiModel = %q, want %q", got.GeminiModel, "claude-opus-4-6")
	}
	if got.ModelID != "anthropic-api-key/claude-opus-4-6" {
		t.Fatalf("ModelID = %q, want %q", got.ModelID, "anthropic-api-key/claude-opus-4-6")
	}
}

func TestNormalizeSettingsSupportsAnthropicSetupTokenProvider(t *testing.T) {
	in := Settings{
		ProviderType: ProviderOAuth,
		ProviderID:   "anthropic-setup-token",
		ModelID:      "anthropic-setup-token/claude-sonnet-4-6",
		GeminiModel:  "claude-sonnet-4-6",
	}
	raw := map[string]json.RawMessage{
		"provider_type": json.RawMessage(`"oauth"`),
		"provider_id":   json.RawMessage(`"anthropic-setup-token"`),
		"model_id":      json.RawMessage(`"anthropic-setup-token/claude-sonnet-4-6"`),
	}

	got := normalizeSettings(in, raw)
	if got.ProviderID != "anthropic-setup-token" {
		t.Fatalf("ProviderID = %q, want %q", got.ProviderID, "anthropic-setup-token")
	}
	if got.ProviderType != ProviderOAuth {
		t.Fatalf("ProviderType = %q, want %q", got.ProviderType, ProviderOAuth)
	}
	if got.ModelID != "anthropic-setup-token/claude-sonnet-4-6" {
		t.Fatalf("ModelID = %q, want %q", got.ModelID, "anthropic-setup-token/claude-sonnet-4-6")
	}
}
