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
