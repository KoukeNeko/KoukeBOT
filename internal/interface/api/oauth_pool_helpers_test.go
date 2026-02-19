package api

import (
	"path/filepath"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestPersistOAuthProviderSelectionResetsMismatchedModelProvider(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	store := config.NewSettingsStore(settingsPath)

	if err := store.Save(config.Settings{
		ProviderType: config.ProviderOAuth,
		ProviderID:   llm.ProviderAIStudioRPC,
		ModelID:      llm.BuildProviderModelID(llm.ProviderAIStudioRPC, "gemini-3-pro-preview"),
		GeminiModel:  "gemini-3-pro-preview",
	}); err != nil {
		t.Fatalf("save initial settings: %v", err)
	}

	h := &Handlers{settingsStore: store}
	h.persistOAuthProviderSelection(llm.ProviderGoogleGeminiCLI)

	updated, err := store.Load()
	if err != nil {
		t.Fatalf("load updated settings: %v", err)
	}

	if updated.ProviderType != config.ProviderOAuth {
		t.Fatalf("ProviderType = %q, want %q", updated.ProviderType, config.ProviderOAuth)
	}
	if updated.ProviderID != llm.ProviderGoogleGeminiCLI {
		t.Fatalf("ProviderID = %q, want %q", updated.ProviderID, llm.ProviderGoogleGeminiCLI)
	}
	wantModelID := llm.DefaultProviderModelID(llm.ProviderGoogleGeminiCLI)
	if updated.ModelID != wantModelID {
		t.Fatalf("ModelID = %q, want %q", updated.ModelID, wantModelID)
	}
	if updated.GeminiModel != "gemini-3-pro-preview" {
		t.Fatalf("GeminiModel = %q, want %q", updated.GeminiModel, "gemini-3-pro-preview")
	}
}

func TestPersistOAuthProviderSelectionKeepsModelWithinSameProvider(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	store := config.NewSettingsStore(settingsPath)

	customModelID := llm.BuildProviderModelID(llm.ProviderGoogleGeminiCLI, "gemini-2.5-pro")
	if err := store.Save(config.Settings{
		ProviderType: config.ProviderOAuth,
		ProviderID:   llm.ProviderGoogleGeminiCLI,
		ModelID:      customModelID,
		GeminiModel:  "gemini-2.5-pro",
	}); err != nil {
		t.Fatalf("save initial settings: %v", err)
	}

	h := &Handlers{settingsStore: store}
	h.persistOAuthProviderSelection(llm.ProviderGoogleGeminiCLI)

	updated, err := store.Load()
	if err != nil {
		t.Fatalf("load updated settings: %v", err)
	}

	if updated.ProviderID != llm.ProviderGoogleGeminiCLI {
		t.Fatalf("ProviderID = %q, want %q", updated.ProviderID, llm.ProviderGoogleGeminiCLI)
	}
	if updated.ModelID != customModelID {
		t.Fatalf("ModelID = %q, want %q", updated.ModelID, customModelID)
	}
	if updated.GeminiModel != "gemini-2.5-pro" {
		t.Fatalf("GeminiModel = %q, want %q", updated.GeminiModel, "gemini-2.5-pro")
	}
}

