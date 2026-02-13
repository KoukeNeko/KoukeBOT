package config

import "testing"

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
