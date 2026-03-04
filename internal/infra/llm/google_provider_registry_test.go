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

func TestNormalizeGoogleProviderIDSupportsAnthropicProviders(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "anthropic-api-key", want: ProviderAnthropicAPIKey},
		{in: "anthropic-setup-token", want: ProviderAnthropicSetupToken},
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

func TestDefaultProviderModelIDForOpenAIAPIKey(t *testing.T) {
	got := DefaultProviderModelID(ProviderOpenAIAPIKey)
	want := "openai-api-key/gpt-5"
	if got != want {
		t.Fatalf("DefaultProviderModelID(%q) = %q, want %q", ProviderOpenAIAPIKey, got, want)
	}
}

func TestDefaultProviderModelIDForAnthropicProviders(t *testing.T) {
	cases := []struct {
		providerID string
		want       string
	}{
		{providerID: ProviderAnthropicAPIKey, want: "anthropic-api-key/claude-sonnet-4-6"},
		{providerID: ProviderAnthropicSetupToken, want: "anthropic-setup-token/claude-sonnet-4-6"},
	}

	for _, tc := range cases {
		if got := DefaultProviderModelID(tc.providerID); got != tc.want {
			t.Fatalf("DefaultProviderModelID(%q) = %q, want %q", tc.providerID, got, tc.want)
		}
	}
}

func TestNormalizeModelAliasForProviderAnthropicSkipsGoogleClaudeAlias(t *testing.T) {
	got := NormalizeModelAliasForProvider(ProviderAnthropicAPIKey, "claude-opus-4-6")
	if got != "claude-opus-4-6" {
		t.Fatalf("NormalizeModelAliasForProvider(anthropic-api-key, claude-opus-4-6) = %q, want %q", got, "claude-opus-4-6")
	}
}
