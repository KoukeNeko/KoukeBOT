package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func TestRefreshIfNeededWithEmptyTokenPath(t *testing.T) {
	originalEndpoint := google.Endpoint
	defer func() {
		google.Endpoint = originalEndpoint
	}()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"access_token": "new-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tokenServer.Close()

	google.Endpoint = oauth2.Endpoint{
		AuthURL:  "https://example.com/auth",
		TokenURL: tokenServer.URL,
	}

	data := &OAuthTokenData{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour),
	}

	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "gemini_oauth.json")

	if err := RefreshIfNeeded(context.Background(), data, ""); err != nil {
		t.Fatalf("RefreshIfNeeded() error = %v", err)
	}

	if data.AccessToken != "new-access-token" {
		t.Fatalf("access token = %q, want new-access-token", data.AccessToken)
	}
	if data.TokenType != "Bearer" {
		t.Fatalf("token type = %q, want Bearer", data.TokenType)
	}
	if !data.Expiry.After(time.Now()) {
		t.Fatalf("expiry was not updated")
	}

	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("token file should not be created when tokenPath is empty, stat err = %v", err)
	}
}

func TestUpdateOAuthPoolTokenByEmail(t *testing.T) {
	poolPath := filepath.Join(t.TempDir(), "oauth_pool.json")
	seed := []*OAuthTokenData{
		{Email: "a@example.com", AccessToken: "token-a"},
		{
			Email:       "b@example.com",
			AccessToken: "token-b",
			UsageStats: &OAuthUsageStats{
				AttemptsTotal:  7,
				SuccessesTotal: 6,
				FailuresTotal:  1,
			},
		},
	}
	if err := SaveOAuthPool(poolPath, seed); err != nil {
		t.Fatalf("SaveOAuthPool() error = %v", err)
	}

	replacement := &OAuthTokenData{Email: "b@example.com", AccessToken: "token-b-new"}
	if err := UpdateOAuthPoolToken(poolPath, "b@example.com", replacement); err != nil {
		t.Fatalf("UpdateOAuthPoolToken() error = %v", err)
	}

	loaded, err := LoadOAuthPool(poolPath)
	if err != nil {
		t.Fatalf("LoadOAuthPool() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("pool size = %d, want 2", len(loaded))
	}
	if loaded[0].AccessToken != "token-a" {
		t.Fatalf("entry[0] access token = %q, want token-a", loaded[0].AccessToken)
	}
	if loaded[1].AccessToken != "token-b-new" {
		t.Fatalf("entry[1] access token = %q, want token-b-new", loaded[1].AccessToken)
	}
	if loaded[1].UsageStats == nil || loaded[1].UsageStats.AttemptsTotal != 7 {
		t.Fatalf("usage stats should be preserved on token update")
	}

	if err := UpdateOAuthPoolToken(poolPath, "missing@example.com", replacement); err == nil {
		t.Fatalf("UpdateOAuthPoolToken() expected error for missing email")
	}
}

func TestUpdateOAuthPoolUsageStatsByEmail(t *testing.T) {
	poolPath := filepath.Join(t.TempDir(), "oauth_pool.json")
	seed := []*OAuthTokenData{
		{Email: "a@example.com", AccessToken: "token-a"},
	}
	if err := SaveOAuthPool(poolPath, seed); err != nil {
		t.Fatalf("SaveOAuthPool() error = %v", err)
	}

	usage := &OAuthUsageStats{
		AttemptsTotal:  11,
		SuccessesTotal: 10,
		FailuresTotal:  1,
		HourlyBuckets: []OAuthUsageBucket{
			{
				HourStart: time.Now().Truncate(time.Hour),
				Attempts:  3,
				Successes: 2,
				Failures:  1,
			},
		},
	}

	if err := UpdateOAuthPoolUsageStats(poolPath, "a@example.com", usage); err != nil {
		t.Fatalf("UpdateOAuthPoolUsageStats() error = %v", err)
	}

	loaded, err := LoadOAuthPool(poolPath)
	if err != nil {
		t.Fatalf("LoadOAuthPool() error = %v", err)
	}
	if loaded[0].UsageStats == nil || loaded[0].UsageStats.AttemptsTotal != 11 {
		t.Fatalf("usage stats update not persisted")
	}
	if err := UpdateOAuthPoolUsageStats(poolPath, "missing@example.com", usage); err == nil {
		t.Fatalf("UpdateOAuthPoolUsageStats() expected error for missing email")
	}
}

func TestStartFlowV2SupportsConcurrentFlows(t *testing.T) {
	flow := NewGeminiOAuthFlow("", "", "http://localhost:8080/api/oauth/gemini/callback")

	first, err := flow.StartFlowV2("client-id", "client-secret", "local", "")
	if err != nil {
		t.Fatalf("first StartFlowV2() error = %v", err)
	}
	second, err := flow.StartFlowV2("client-id", "client-secret", "remote", "https://example.com/api/oauth/gemini/callback")
	if err != nil {
		t.Fatalf("second StartFlowV2() error = %v", err)
	}

	if first.FlowID == second.FlowID {
		t.Fatalf("flow IDs should be unique")
	}
	if first.Mode != "local" {
		t.Fatalf("first mode = %q, want local", first.Mode)
	}
	if second.Mode != "remote" {
		t.Fatalf("second mode = %q, want remote", second.Mode)
	}

	flow.mu.Lock()
	defer flow.mu.Unlock()
	if got := len(flow.flows); got != 2 {
		t.Fatalf("active flows = %d, want 2", got)
	}
}

func TestStartFlowV2CleansExpiredFlows(t *testing.T) {
	flow := NewGeminiOAuthFlow("", "", "http://localhost:8080/api/oauth/gemini/callback")

	flow.mu.Lock()
	flow.flows["expired"] = &oauthFlowState{
		FlowID:    "expired",
		State:     "state",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}
	flow.mu.Unlock()

	if _, err := flow.StartFlowV2("client-id", "client-secret", "local", ""); err != nil {
		t.Fatalf("StartFlowV2() error = %v", err)
	}

	flow.mu.Lock()
	defer flow.mu.Unlock()
	if _, ok := flow.flows["expired"]; ok {
		t.Fatalf("expired flow should be cleaned up")
	}
}
