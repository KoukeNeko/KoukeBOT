package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

type quotaOnlyProvider struct {
	err error
}

func (p *quotaOnlyProvider) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, p.err
}

func (p *quotaOnlyProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, p.err
}

func (p *quotaOnlyProvider) StreamChatMultimodal(context.Context, string, []domain.MultimodalMessage, domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	return nil, p.err
}

func (p *quotaOnlyProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{ProviderName: "oauth-quota", Model: "gemini-2.5-pro"}
}

func (p *quotaOnlyProvider) RegisterTools([]domain.Tool) {}

func TestAIStudioSessionPutGetDeleteAndHotUpdatePool(t *testing.T) {
	const generatePath = "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/GenerateContent"
	const countTokensPath = "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/CountTokens"

	var countTokensCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case generatePath:
			w.Header().Set("Content-Type", "application/json+protobuf")
			_, _ = w.Write([]byte(`[[[[null,"from-aistudio"],"model"]]]`))
		case countTokensPath:
			countTokensCalls++
			w.Header().Set("Content-Type", "application/json+protobuf")
			_, _ = w.Write([]byte(`[5]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "aistudio_session.json")
	settingsStore := config.NewSettingsStore(filepath.Join(dir, "settings.json"))
	if err := settingsStore.Save(config.Settings{
		ProviderType: config.ProviderOAuth,
		ProviderID:   llm.ProviderGoogleAntigravity,
		ModelID:      llm.ProviderGoogleAntigravity + "/gemini-2.5-pro",
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	pool := app.NewProviderPool()
	pool.SetAutoRetryCount(0)
	pool.AddOrReplace(&quotaOnlyProvider{
		err: errors.New(`429 RESOURCE_EXHAUSTED {"quotaResetDelay":"90s"}`),
	}, "quota@example.com")
	providerManager := app.NewProviderManager(pool)

	h := &Handlers{
		providerManager:     providerManager,
		settingsStore:       settingsStore,
		aiStudioSessionPath: sessionPath,
	}

	putPayload := map[string]string{
		"endpoint":             server.URL,
		"api_key":              "api-key",
		"x_browser_validation": "browser-validation",
		"x_client_data":        "client-data",
		"x_goog_authuser":      "0",
		"user_agent":           "Mozilla/test",
	}
	putBody, _ := json.Marshal(putPayload)
	putReq := httptest.NewRequest(http.MethodPut, "/api/aistudio/session", bytes.NewReader(putBody))
	putRec := httptest.NewRecorder()
	h.AIStudioSessionPut(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d body=%s", putRec.Code, http.StatusOK, putRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/aistudio/session", nil)
	getRec := httptest.NewRecorder()
	h.AIStudioSessionGet(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d body=%s", getRec.Code, http.StatusOK, getRec.Body.String())
	}
	var getResp AIStudioSessionStatusResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if !getResp.Configured || getResp.Session == nil {
		t.Fatalf("expected configured session, got %#v", getResp)
	}
	if getResp.Session.APIKey == "api-key" {
		t.Fatalf("API key should be masked in GET response")
	}
	if !strings.HasPrefix(getResp.Session.APIKey, "set(len=") {
		t.Fatalf("unexpected masked API key: %q", getResp.Session.APIKey)
	}
	if !strings.HasPrefix(getResp.Session.XGoogExt519733851, "set(len=") {
		t.Fatalf("unexpected masked X-Goog-Ext header: %q", getResp.Session.XGoogExt519733851)
	}

	chatResp, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "sys",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("pool.Chat() with fallback should succeed, got %v", err)
	}
	if !strings.Contains(chatResp.Content, "from-aistudio") {
		t.Fatalf("unexpected fallback response: %#v", chatResp)
	}

	verifyReq := httptest.NewRequest(http.MethodPost, "/api/aistudio/session/verify", bytes.NewBufferString(`{}`))
	verifyRec := httptest.NewRecorder()
	h.AIStudioSessionVerify(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want %d body=%s", verifyRec.Code, http.StatusOK, verifyRec.Body.String())
	}
	if countTokensCalls == 0 {
		t.Fatalf("expected verify to call CountTokens")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/aistudio/session", nil)
	deleteRec := httptest.NewRecorder()
	h.AIStudioSessionDelete(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d body=%s", deleteRec.Code, http.StatusOK, deleteRec.Body.String())
	}
	if _, err := os.Stat(sessionPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected session file to be removed, err=%v", err)
	}

	_, err = pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "sys",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("expected exhausted error after fallback removal, got: %v", err)
	}
}

func TestAIStudioSessionPutRejectsMissingRequiredFields(t *testing.T) {
	h := &Handlers{
		aiStudioSessionPath: filepath.Join(t.TempDir(), "aistudio_session.json"),
	}

	body := bytes.NewBufferString(`{
		"endpoint":"https://alkalimakersuite-pa.clients6.google.com",
		"x_client_data":"client",
		"x_goog_authuser":"0",
		"user_agent":"Mozilla/test"
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/aistudio/session", body)
	rec := httptest.NewRecorder()
	h.AIStudioSessionPut(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing x_browser_validation") {
		t.Fatalf("expected missing x_browser_validation error, got: %s", rec.Body.String())
	}
}

func TestAIStudioSessionVerifyInvalidSession(t *testing.T) {
	h := &Handlers{
		aiStudioSessionPath: filepath.Join(t.TempDir(), "aistudio_session.json"),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/aistudio/session/verify", bytes.NewBufferString(`{
		"session": {
			"endpoint":"https://alkalimakersuite-pa.clients6.google.com",
			"x_client_data":"client",
			"x_goog_authuser":"0",
			"user_agent":"Mozilla/test"
		}
	}`))
	rec := httptest.NewRecorder()
	h.AIStudioSessionVerify(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	verified, _ := resp["verified"].(bool)
	if verified {
		t.Fatalf("expected verify=false for invalid session, got %#v", resp)
	}
	if !strings.Contains(strings.ToLower(toString(resp["error"])), "x_browser_validation") {
		t.Fatalf("expected x_browser_validation validation error, got %#v", resp)
	}
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
