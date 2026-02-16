package llm

import (
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestResolveMaxTokensForModelThinkingFloor(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		configured int
		want      int
	}{
		{"gemini-2.5-pro low config", "gemini-2.5-pro", 4096, minThinkingModelOutputTokens},
		{"gemini-2.5-flash low config", "gemini-2.5-flash", 4096, minThinkingModelOutputTokens},
		{"gemini-3-pro-preview low config", "gemini-3-pro-preview", 4096, minThinkingModelOutputTokens},
		{"gemini-3-flash-preview low config", "gemini-3-flash-preview", 4096, minThinkingModelOutputTokens},
		{"gemini-2.5-pro high config preserved", "gemini-2.5-pro", 32768, 32768},
		{"gemini-2.0-flash no floor", "gemini-2.0-flash", 4096, 4096},
		{"gemini-1.5-pro no floor", "gemini-1.5-pro", 4096, 4096},
		{"gemini-2.0-flash-lite no floor", "gemini-2.0-flash-lite", 2048, 2048},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveMaxTokensForModel(tc.model, tc.configured)
			if got != tc.want {
				t.Fatalf("resolveMaxTokensForModel(%q, %d) = %d, want %d", tc.model, tc.configured, got, tc.want)
			}
		})
	}
}

func TestBuildConfigAppliesThinkingFloor(t *testing.T) {
	req := ChatRequest{
		Generation: domain.GenerationConfig{
			MaxTokens: 4096,
		},
	}

	thinkingCfg := buildConfig(req, "gemini-2.5-pro")
	if thinkingCfg.maxTokens != minThinkingModelOutputTokens {
		t.Fatalf("thinking model maxTokens = %d, want %d", thinkingCfg.maxTokens, minThinkingModelOutputTokens)
	}

	legacyCfg := buildConfig(req, "gemini-2.0-flash")
	if legacyCfg.maxTokens != 4096 {
		t.Fatalf("legacy model maxTokens = %d, want 4096", legacyCfg.maxTokens)
	}
}

func TestBuildGenerationConfigAppliesThinkingFloor(t *testing.T) {
	gen := domain.GenerationConfig{
		MaxTokens: 4096,
	}

	thinkingCfg := buildGenerationConfig(gen, "gemini-3-pro-preview")
	if thinkingCfg.maxTokens != minThinkingModelOutputTokens {
		t.Fatalf("thinking model maxTokens = %d, want %d", thinkingCfg.maxTokens, minThinkingModelOutputTokens)
	}

	legacyCfg := buildGenerationConfig(gen, "gemini-1.5-flash")
	if legacyCfg.maxTokens != 4096 {
		t.Fatalf("legacy model maxTokens = %d, want 4096", legacyCfg.maxTokens)
	}
}
