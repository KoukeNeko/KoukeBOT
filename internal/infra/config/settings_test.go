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
}

func TestNormalizeSettingsKeepsExplicitFalseValues(t *testing.T) {
	explicitFalse := json.RawMessage("false")
	debugValue := json.RawMessage(`"debug"`)
	constraintValue := json.RawMessage(`"keep this"`)
	raw := map[string]json.RawMessage{
		"triplet_output_enabled":      explicitFalse,
		"negative_constraint_enabled": explicitFalse,
		"analysis_visibility":         debugValue,
		"negative_constraint_default": constraintValue,
	}
	in := Settings{
		PersonaLockEveryTurns:     6,
		TripletOutputEnabled:      false,
		NegativeConstraintEnabled: false,
		AnalysisVisibility:        "debug",
		NegativeConstraintDefault: "keep this",
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
}
