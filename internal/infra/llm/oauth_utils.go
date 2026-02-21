package llm

// oauth_utils.go — Stateless OAuth helper utilities: PKCE generation, token
// exchange, auth URL construction, and the shared HTTP client.
//
// These helpers were previously split across oauth_pkce.go, oauth_exchange.go,
// and oauth_http_client.go. They are merged here because they are all small,
// stateless primitives used exclusively by the OAuth flow initiation code,
// and keeping them together reduces the number of files without mixing concerns.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// oauthHTTPDoer abstracts the HTTP client for testability.
type oauthHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

var oauthHTTPClient oauthHTTPDoer = http.DefaultClient

// generateRandomHex generates a cryptographically random hex string of the given byte length.
func generateRandomHex(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto/rand read: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// generatePKCE generates a PKCE verifier and S256 challenge pair.
func generatePKCE() (verifier, challenge string, err error) {
	verifier, err = generateRandomHex(32)
	if err != nil {
		return "", "", fmt.Errorf("generate PKCE verifier: %w", err)
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, nil
}

// generateState generates a random OAuth state parameter to prevent CSRF.
func generateState() (string, error) {
	return generateRandomHex(16)
}

// generateFlowID generates a unique identifier for an OAuth flow session.
func generateFlowID() (string, error) {
	return generateRandomHex(16)
}

// buildAuthURL constructs the Google OAuth2 authorization URL with PKCE parameters.
func buildAuthURL(clientID, redirectURL, state, codeVerifier string, scopes []string) string {
	challenge := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(challenge[:])
	if len(scopes) == 0 {
		scopes = oauthScopes
	}

	params := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURL},
		"scope":                 {strings.Join(scopes, " ")},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}

	return oauthAuthURL + "?" + params.Encode()
}

// exchangeCode performs the OAuth2 authorization code exchange with PKCE.
func exchangeCode(ctx context.Context, clientID, clientSecret, code, verifier, redirectURI string) (*oauth2.Token, error) {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURI,
	}
	return cfg.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
}

// fetchUserEmail retrieves the Google account email for the given access token.
func fetchUserEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthUserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("decode user info response: %w", err)
	}
	return info.Email, nil
}
