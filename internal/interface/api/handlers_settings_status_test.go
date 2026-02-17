package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestGetProviderStatusReturnsModelField(t *testing.T) {
	providerManager := app.NewProviderManager(&statusStubProvider{
		info: llm.ModelCapabilities{
			ProviderName: "gemini-oauth-pool",
			Model:        "gemini-2.5-pro",
		},
	})
	h := &Handlers{providerManager: providerManager}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings/status", nil)
	h.GetProviderStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp ProviderStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ProviderName != "gemini-oauth-pool" {
		t.Fatalf("provider_name = %q, want gemini-oauth-pool", resp.ProviderName)
	}
	if resp.Model != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want gemini-2.5-pro", resp.Model)
	}
}

func TestListModelsPrefersOAuthModeOverAPIKey(t *testing.T) {
	dir := t.TempDir()
	settingsStore := config.NewSettingsStore(filepath.Join(dir, "settings.json"))
	if err := settingsStore.Save(config.Settings{
		ProviderType: config.ProviderOAuth,
		GeminiAPIKey: "api-key-should-not-be-used-for-listing",
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	h := &Handlers{
		settingsStore: settingsStore,
		oauthFlow: llm.NewGeminiOAuthFlow(
			filepath.Join(dir, "oauth.json"),
			filepath.Join(dir, "pool.json"),
			"http://127.0.0.1:8080/api/oauth/gemini/callback",
		),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	h.ListModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var models []llm.ModelEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(models) == 0 {
		t.Fatalf("expected non-empty model list")
	}
	for _, model := range models {
		if strings.HasPrefix(model.ID, "models/") {
			t.Fatalf("model id should be normalized, got %q", model.ID)
		}
	}
}

func TestListModelsRespectsProviderIDQueryOverride(t *testing.T) {
	dir := t.TempDir()
	settingsStore := config.NewSettingsStore(filepath.Join(dir, "settings.json"))
	if err := settingsStore.Save(config.Settings{
		ProviderType: config.ProviderOAuth,
		ProviderID:   llm.ProviderGoogleGeminiCLI,
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	h := &Handlers{
		settingsStore: settingsStore,
		oauthFlow: llm.NewGeminiOAuthFlow(
			filepath.Join(dir, "oauth.json"),
			filepath.Join(dir, "pool.json"),
			"http://127.0.0.1:8080/api/oauth/gemini/callback",
		),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/models?provider_id=google-antigravity", nil)
	h.ListModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var models []llm.ModelEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(models) == 0 {
		t.Fatalf("expected non-empty model list")
	}
	for _, model := range models {
		if !strings.HasPrefix(model.ID, llm.ProviderGoogleAntigravity+"/") {
			t.Fatalf("model id = %q, want %q prefix", model.ID, llm.ProviderGoogleAntigravity+"/")
		}
	}
}

type statusStubProvider struct {
	info llm.ModelCapabilities
}

func (s *statusStubProvider) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (s *statusStubProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (s *statusStubProvider) StreamChatMultimodal(context.Context, string, []domain.MultimodalMessage, domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (s *statusStubProvider) ModelInfo() llm.ModelCapabilities {
	return s.info
}

func (s *statusStubProvider) RegisterTools([]domain.Tool) {}
