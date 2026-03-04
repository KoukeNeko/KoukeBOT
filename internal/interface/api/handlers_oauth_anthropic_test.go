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
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func newAnthropicSetupTokenForTest() string {
	return llm.AnthropicSetupTokenPrefix + strings.Repeat("a", 96)
}

func newAnthropicOAuthHandlersForTest(t *testing.T) (*Handlers, string) {
	t.Helper()
	dir := t.TempDir()
	settingsStore := config.NewSettingsStore(filepath.Join(dir, "settings.json"))
	if err := settingsStore.Save(config.Settings{}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	h := &Handlers{
		settingsStore:   settingsStore,
		providerManager: app.NewProviderManager(nil),
		oauthFlow: llm.NewGeminiOAuthFlow(
			filepath.Join(dir, "oauth.json"),
			filepath.Join(dir, "oauth_pool.json"),
			"http://127.0.0.1:8080/api/oauth/gemini/callback",
		),
		ctx: context.Background(),
	}
	return h, h.oauthFlow.PoolPath()
}

func TestOAuthAnthropicImportTokenSuccess(t *testing.T) {
	h, poolPath := newAnthropicOAuthHandlersForTest(t)
	token := newAnthropicSetupTokenForTest()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/anthropic/import-token", strings.NewReader(`{"setup_token":"`+token+`"}`))
	h.OAuthAnthropicImportToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["provider_id"] != llm.ProviderAnthropicSetupToken {
		t.Fatalf("provider_id = %q, want %q", resp["provider_id"], llm.ProviderAnthropicSetupToken)
	}
	if !strings.HasPrefix(resp["email"], "anthropic:setup_token_") {
		t.Fatalf("email = %q, want anthropic setup token summary", resp["email"])
	}

	tokens, err := llm.LoadOAuthPool(poolPath)
	if err != nil {
		t.Fatalf("load pool: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("pool size = %d, want 1", len(tokens))
	}
	if got := llm.NormalizeGoogleProviderID(tokens[0].ProviderID); got != llm.ProviderAnthropicSetupToken {
		t.Fatalf("token provider = %q, want %q", got, llm.ProviderAnthropicSetupToken)
	}
	if tokens[0].AccessToken != token {
		t.Fatalf("access token mismatch")
	}
}

func TestOAuthAnthropicImportTokenRejectsInvalidToken(t *testing.T) {
	h, _ := newAnthropicOAuthHandlersForTest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/anthropic/import-token", strings.NewReader(`{"setup_token":"invalid"}`))
	h.OAuthAnthropicImportToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestOAuthAnthropicImportEnv(t *testing.T) {
	t.Setenv("CLAUDE_SETUP_TOKEN", newAnthropicSetupTokenForTest())
	t.Setenv("ANTHROPIC_SETUP_TOKEN", "")

	h, poolPath := newAnthropicOAuthHandlersForTest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/anthropic/import-env", nil)
	h.OAuthAnthropicImportEnv(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	tokens, err := llm.LoadOAuthPool(poolPath)
	if err != nil {
		t.Fatalf("load pool: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("pool size = %d, want 1", len(tokens))
	}
}

func TestOAuthAnthropicImportEnvMissingToken(t *testing.T) {
	t.Setenv("CLAUDE_SETUP_TOKEN", "")
	t.Setenv("ANTHROPIC_SETUP_TOKEN", "")

	h, _ := newAnthropicOAuthHandlersForTest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/anthropic/import-env", nil)
	h.OAuthAnthropicImportEnv(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestOAuthAnthropicStatus(t *testing.T) {
	h, _ := newAnthropicOAuthHandlersForTest(t)
	token := newAnthropicSetupTokenForTest()

	importReq := httptest.NewRequest(http.MethodPost, "/api/oauth/anthropic/import-token", strings.NewReader(`{"setup_token":"`+token+`"}`))
	importRec := httptest.NewRecorder()
	h.OAuthAnthropicImportToken(importRec, importReq)
	if importRec.Code != http.StatusOK {
		t.Fatalf("import status = %d, want %d body=%s", importRec.Code, http.StatusOK, importRec.Body.String())
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/anthropic/status", nil)
	h.OAuthAnthropicStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp OAuthStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Authenticated {
		t.Fatalf("authenticated = false, want true")
	}
	if resp.ProviderID != llm.ProviderAnthropicSetupToken {
		t.Fatalf("provider_id = %q, want %q", resp.ProviderID, llm.ProviderAnthropicSetupToken)
	}
}
