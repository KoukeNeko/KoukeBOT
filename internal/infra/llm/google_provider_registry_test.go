package llm

import "testing"

func TestNormalizeGoogleModelAliasMapsLegacyClaudeIDs(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "claude-opus-4-6", want: "claude-opus-4-6-thinking"},
		{in: "models/claude-opus-4-6", want: "claude-opus-4-6-thinking"},
		{in: "google-antigravity/claude-sonnet-4", want: "claude-sonnet-4-thinking"},
	}

	for _, tt := range tests {
		if got := NormalizeGoogleModelAlias(tt.in); got != tt.want {
			t.Fatalf("NormalizeGoogleModelAlias(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveProviderAndModelNormalizesLegacyClaudeModel(t *testing.T) {
	providerID, model, modelID := ResolveProviderAndModel(ProviderGoogleAntigravity, "", "claude-opus-4-6")
	if providerID != ProviderGoogleAntigravity {
		t.Fatalf("providerID = %q, want %q", providerID, ProviderGoogleAntigravity)
	}
	if model != "claude-opus-4-6-thinking" {
		t.Fatalf("model = %q, want %q", model, "claude-opus-4-6-thinking")
	}
	if modelID != "google-antigravity/claude-opus-4-6-thinking" {
		t.Fatalf("modelID = %q, want %q", modelID, "google-antigravity/claude-opus-4-6-thinking")
	}
}

func TestNormalizeGoogleProviderIDSupportsAIStudioRPCAliases(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "aistudio-rpc", want: ProviderAIStudioRPC},
		{in: "aistudio", want: ProviderAIStudioRPC},
		{in: "ai-studio", want: ProviderAIStudioRPC},
	}

	for _, tt := range tests {
		if got := NormalizeGoogleProviderID(tt.in); got != tt.want {
			t.Fatalf("NormalizeGoogleProviderID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDefaultProviderModelIDForAIStudioRPC(t *testing.T) {
	got := DefaultProviderModelID(ProviderAIStudioRPC)
	want := "aistudio-rpc/gemini-3-pro-preview"
	if got != want {
		t.Fatalf("DefaultProviderModelID(%q) = %q, want %q", ProviderAIStudioRPC, got, want)
	}
}
