package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestParseAIStudioFinalAnswerPrefersLastModelSegment(t *testing.T) {
	raw, err := json.Marshal([]any{
		[]any{[]any{nil, "**thinking**"}, "model"},
		[]any{[]any{nil, "final answer"}, "model"},
	})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	got, err := parseAIStudioFinalAnswer(raw)
	if err != nil {
		t.Fatalf("parseAIStudioFinalAnswer() error = %v", err)
	}
	if got != "final answer" {
		t.Fatalf("answer = %q, want %q", got, "final answer")
	}
}

func TestAIStudioRPCProviderChatAndStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != aiStudioRPCGenerateContentPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, aiStudioRPCGenerateContentPath)
		}
		if r.Header.Get("X-Goog-Api-Key") == "" {
			t.Fatalf("missing X-Goog-Api-Key header")
		}
		if r.Header.Get("X-Browser-Validation") == "" {
			t.Fatalf("missing X-Browser-Validation header")
		}
		if r.Header.Get("X-Goog-Ext-519733851-Bin") != defaultAIStudioGoogExt519733851 {
			t.Fatalf("unexpected X-Goog-Ext-519733851-Bin header: %q", r.Header.Get("X-Goog-Ext-519733851-Bin"))
		}
		w.Header().Set("Content-Type", "application/json+protobuf")
		_, _ = w.Write([]byte(`[[[[null,"OK"],"model"]]]`))
	}))
	defer server.Close()

	provider, err := NewAIStudioRPCProvider(&AIStudioSession{
		Endpoint:           server.URL,
		APIKey:             "api-key",
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		UserAgent:          "Mozilla/test",
	}, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("NewAIStudioRPCProvider() error = %v", err)
	}

	resp, err := provider.Chat(context.Background(), ChatRequest{
		SystemPrompt: "sys",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if strings.TrimSpace(resp.Content) != "OK" {
		t.Fatalf("chat content = %q, want %q", resp.Content, "OK")
	}

	stream, err := provider.StreamChat(context.Background(), ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}

	var joined strings.Builder
	sawDone := false
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
		if chunk.Content != "" {
			joined.WriteString(chunk.Content)
		}
		if chunk.Done {
			sawDone = true
		}
	}
	if strings.TrimSpace(joined.String()) != "OK" {
		t.Fatalf("stream content = %q, want %q", joined.String(), "OK")
	}
	if !sawDone {
		t.Fatal("expected stream Done chunk")
	}
}

func TestAIStudioRPCProviderStreamChatMultimodalUnsupported(t *testing.T) {
	provider, err := NewAIStudioRPCProvider(&AIStudioSession{
		Endpoint:           "https://example.com",
		APIKey:             "api-key",
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		UserAgent:          "Mozilla/test",
	}, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("NewAIStudioRPCProvider() error = %v", err)
	}

	_, err = provider.StreamChatMultimodal(context.Background(), "sys", nil, domain.GenerationConfig{})
	if err == nil {
		t.Fatal("expected multimodal unsupported error")
	}
}

func TestVerifyAIStudioSessionUsesCountTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != aiStudioRPCCountTokensPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, aiStudioRPCCountTokensPath)
		}
		w.Header().Set("Content-Type", "application/json+protobuf")
		_, _ = w.Write([]byte(`[5,[],[[[4],1]],null,null,[[1,5]]]`))
	}))
	defer server.Close()

	err := VerifyAIStudioSession(context.Background(), &AIStudioSession{
		Endpoint:           server.URL,
		APIKey:             "api-key",
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		UserAgent:          "Mozilla/test",
	}, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("VerifyAIStudioSession() error = %v", err)
	}
}

func TestVerifyAIStudioSessionAutoRefreshesAccessToken(t *testing.T) {
	var tokenCalls int
	var countTokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case aiStudioRPCGenerateAccessToken:
			tokenCalls++
			w.Header().Set("Content-Type", "application/json+protobuf")
			_, _ = w.Write([]byte(`["ya29.token-1"]`))
		case aiStudioRPCCountTokensPath:
			countTokenCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer ya29.token-1" {
				t.Fatalf("authorization = %q, want %q", got, "Bearer ya29.token-1")
			}
			w.Header().Set("Content-Type", "application/json+protobuf")
			_, _ = w.Write([]byte(`[5,[],[[[4],1]],null,null,[[1,5]]]`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	err := VerifyAIStudioSession(context.Background(), &AIStudioSession{
		Endpoint:           server.URL,
		APIKey:             "api-key",
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		Cookie:             "SID=abc",
		UserAgent:          "Mozilla/test",
	}, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("VerifyAIStudioSession() error = %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("GenerateAccessToken calls = %d, want 1", tokenCalls)
	}
	if countTokenCalls != 1 {
		t.Fatalf("CountTokens calls = %d, want 1", countTokenCalls)
	}
}

func TestVerifyAIStudioSessionRefreshesTokenAfterUnauthorized(t *testing.T) {
	var tokenCalls int
	var countTokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case aiStudioRPCGenerateAccessToken:
			tokenCalls++
			w.Header().Set("Content-Type", "application/json+protobuf")
			if tokenCalls == 1 {
				_, _ = w.Write([]byte(`["ya29.token-old"]`))
				return
			}
			_, _ = w.Write([]byte(`["ya29.token-new"]`))
		case aiStudioRPCCountTokensPath:
			countTokenCalls++
			auth := r.Header.Get("Authorization")
			if auth == "Bearer ya29.token-old" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`[16,"expired"]`))
				return
			}
			if auth != "Bearer ya29.token-new" {
				t.Fatalf("authorization = %q, want %q", auth, "Bearer ya29.token-new")
			}
			w.Header().Set("Content-Type", "application/json+protobuf")
			_, _ = w.Write([]byte(`[5,[],[[[4],1]],null,null,[[1,5]]]`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	err := VerifyAIStudioSession(context.Background(), &AIStudioSession{
		Endpoint:           server.URL,
		APIKey:             "api-key",
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		Cookie:             "SID=abc",
		UserAgent:          "Mozilla/test",
	}, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("VerifyAIStudioSession() error = %v", err)
	}
	if tokenCalls != 2 {
		t.Fatalf("GenerateAccessToken calls = %d, want 2", tokenCalls)
	}
	if countTokenCalls != 2 {
		t.Fatalf("CountTokens calls = %d, want 2", countTokenCalls)
	}
}

func TestNewAIStudioRPCProviderFallsBackToGeminiModelForNonGeminiInput(t *testing.T) {
	provider, err := NewAIStudioRPCProvider(&AIStudioSession{
		Endpoint:           "https://example.com",
		APIKey:             "api-key",
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		UserAgent:          "Mozilla/test",
	}, "claude-opus-4-6-thinking")
	if err != nil {
		t.Fatalf("NewAIStudioRPCProvider() error = %v", err)
	}

	if got := provider.ModelInfo().Model; got != "gemini-3-pro-preview" {
		t.Fatalf("model = %q, want %q", got, "gemini-3-pro-preview")
	}
}
