package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type mockOAuthDoer struct {
	do func(req *http.Request) (*http.Response, error)
}

func (m mockOAuthDoer) Do(req *http.Request) (*http.Response, error) {
	return m.do(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestVerifyOAuthAccountSuccess(t *testing.T) {
	originalClient := oauthHTTPClient
	defer func() { oauthHTTPClient = originalClient }()

	oauthHTTPClient = mockOAuthDoer{
		do: func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/oauth2/v1/userinfo":
				return jsonResponse(http.StatusOK, `{"email":"verified@example.com"}`), nil
			case "/v1internal:loadCodeAssist":
				return jsonResponse(http.StatusOK, `{
					"currentTier": "free-tier",
					"cloudaicompanionProject": {"id":"verified-project"}
				}`), nil
			default:
				return jsonResponse(http.StatusNotFound, `{}`), nil
			}
		},
	}

	ssePayload := "data: " +
		`{"response":{"candidates":[{"content":{"parts":[{"text":"OK"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":3}}}` +
		"\n\n"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "cloudcode-pa.googleapis.com" && req.URL.Path == "/v1internal:streamGenerateContent" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(ssePayload)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("{}")),
			}, nil
		}),
	}

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)
	tokenData := &OAuthTokenData{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(30 * time.Minute),
	}

	result, err := VerifyOAuthAccount(ctx, tokenData, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("VerifyOAuthAccount() error = %v", err)
	}
	if result == nil {
		t.Fatalf("VerifyOAuthAccount() returned nil result")
	}
	if result.Email != "verified@example.com" {
		t.Fatalf("result email = %q, want verified@example.com", result.Email)
	}
	if result.ProjectID != "verified-project" {
		t.Fatalf("result project = %q, want verified-project", result.ProjectID)
	}
	if strings.TrimSpace(result.Preview) == "" {
		t.Fatalf("result preview should not be empty")
	}
}

func TestVerifyOAuthAccountRequiresRefreshToken(t *testing.T) {
	_, err := VerifyOAuthAccount(context.Background(), &OAuthTokenData{}, "gemini-2.5-pro")
	if err == nil {
		t.Fatal("expected missing refresh_token error")
	}
	if !strings.Contains(err.Error(), "refresh_token") {
		t.Fatalf("error = %q, want refresh_token", err.Error())
	}
}
