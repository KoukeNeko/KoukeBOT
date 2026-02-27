package bootstrap

import (
	"path/filepath"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestResolveSettingsUsesOpenAIEnvWhenGeminiKeyMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("OPENAI_MODEL", "gpt-5-mini")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GEMINI_MODEL", "")

	store := config.NewSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Save(config.Settings{}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	got := ResolveSettings(store, Loggers{})
	if got.ProviderType != config.ProviderAPIKey {
		t.Fatalf("ProviderType = %q, want %q", got.ProviderType, config.ProviderAPIKey)
	}
	if got.ProviderID != llm.ProviderOpenAIAPIKey {
		t.Fatalf("ProviderID = %q, want %q", got.ProviderID, llm.ProviderOpenAIAPIKey)
	}
	if got.ModelID != llm.BuildProviderModelID(llm.ProviderOpenAIAPIKey, "gpt-5-mini") {
		t.Fatalf("ModelID = %q", got.ModelID)
	}
}

func TestResolveSettingsPrefersGeminiWhenBothAPIKeysExist(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("OPENAI_MODEL", "gpt-5-mini")
	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_MODEL", "gemini-2.5-pro")

	store := config.NewSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Save(config.Settings{}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	got := ResolveSettings(store, Loggers{})
	if got.ProviderID != llm.ProviderGeminiAPIKey {
		t.Fatalf("ProviderID = %q, want %q", got.ProviderID, llm.ProviderGeminiAPIKey)
	}
}
