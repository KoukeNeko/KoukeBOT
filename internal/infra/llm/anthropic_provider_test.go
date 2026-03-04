package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestValidateAnthropicSetupToken(t *testing.T) {
	valid := AnthropicSetupTokenPrefix + strings.Repeat("a", 90)
	if err := ValidateAnthropicSetupToken(valid); err != nil {
		t.Fatalf("ValidateAnthropicSetupToken(valid) error = %v", err)
	}
	if err := ValidateAnthropicSetupToken("invalid"); err == nil {
		t.Fatalf("ValidateAnthropicSetupToken(invalid) expected error")
	}
}

func TestAnthropicProviderHeadersAPIKey(t *testing.T) {
	var seenAuth, seenAPIKey, seenBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenAPIKey = r.Header.Get("x-api-key")
		seenBeta = r.Header.Get("anthropic-beta")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{
				"input_tokens":  1,
				"output_tokens": 1,
			},
			"stop_reason": "end_turn",
		})
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	p, err := NewAnthropicAPIProvider("sk-ant-api-test", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("NewAnthropicAPIProvider error = %v", err)
	}

	_, err = p.Chat(context.Background(), ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if seenAuth != "" {
		t.Fatalf("Authorization header = %q, want empty", seenAuth)
	}
	if seenAPIKey != "sk-ant-api-test" {
		t.Fatalf("x-api-key = %q, want %q", seenAPIKey, "sk-ant-api-test")
	}
	if !strings.Contains(seenBeta, "fine-grained-tool-streaming-2025-05-14") {
		t.Fatalf("anthropic-beta = %q, want default beta", seenBeta)
	}
}

func TestAnthropicProviderHeadersSetupToken(t *testing.T) {
	var seenAuth, seenAPIKey, seenBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenAPIKey = r.Header.Get("x-api-key")
		seenBeta = r.Header.Get("anthropic-beta")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{
				"input_tokens":  1,
				"output_tokens": 1,
			},
			"stop_reason": "end_turn",
		})
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	token := AnthropicSetupTokenPrefix + strings.Repeat("b", 90)
	p, err := NewAnthropicSetupTokenProvider(token, "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("NewAnthropicSetupTokenProvider error = %v", err)
	}

	_, err = p.Chat(context.Background(), ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if seenAuth != "Bearer "+token {
		t.Fatalf("Authorization = %q, want bearer token", seenAuth)
	}
	if seenAPIKey != "" {
		t.Fatalf("x-api-key = %q, want empty", seenAPIKey)
	}
	if !strings.Contains(seenBeta, "oauth-2025-04-20") {
		t.Fatalf("anthropic-beta = %q, want oauth beta", seenBeta)
	}
}

func TestAnthropicProviderErrorStatusFormatting(t *testing.T) {
	statuses := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError}
	for _, status := range statuses {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"type":"error","message":"boom"}}`))
		}))

		t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
		p, err := NewAnthropicAPIProvider("sk-ant-api-test", "claude-sonnet-4-6")
		if err != nil {
			srv.Close()
			t.Fatalf("NewAnthropicAPIProvider error = %v", err)
		}
		_, err = p.Chat(context.Background(), ChatRequest{
			Messages: []domain.Message{{Role: domain.RoleUser, Content: "ping"}},
		})
		srv.Close()

		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if !strings.Contains(err.Error(), "anthropic HTTP "+strconv.Itoa(status)) {
			t.Fatalf("status %d: error = %q, want HTTP status marker", status, err.Error())
		}
	}
}

func TestAnthropicResponseToResultParsesToolUse(t *testing.T) {
	resp := &anthropicMessagesResponse{
		Content: []anthropicContentPart{
			{Type: "text", Text: "hello"},
			{Type: "tool_use", ID: "call_1", Name: "web_search", Input: map[string]interface{}{"q": "taipei"}},
		},
	}
	text, calls := anthropicResponseToResult(resp)
	if text != "hello" {
		t.Fatalf("text = %q, want %q", text, "hello")
	}
	if len(calls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Fatalf("tool call name = %q, want %q", calls[0].Name, "web_search")
	}
}
