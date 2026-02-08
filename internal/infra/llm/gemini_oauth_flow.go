package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// OAuth constants matching openclaw/Gemini CLI implementation.
const (
	oauthAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	oauthTokenURL    = "https://oauth2.googleapis.com/token"
	oauthUserInfoURL = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	codeAssistURL    = "https://cloudcode-pa.googleapis.com"

	// Token expiry safety margin (5 minutes before actual expiry).
	tokenExpiryMargin = 5 * time.Minute

	// Code Assist operation polling.
	pollMaxAttempts = 24
	pollInterval    = 5 * time.Second
)

var oauthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	// Note: cclog and experimentsandconfigs scopes are not available
	// for the OAuth client extracted from Gemini CLI (Error 403: restricted_client)
}

// Regex patterns to extract OAuth credentials from Gemini CLI source.
var (
	clientIDRegex     = regexp.MustCompile(`(\d+-[a-z0-9]+\.apps\.googleusercontent\.com)`)
	clientSecretRegex = regexp.MustCompile(`(GOCSPX-[A-Za-z0-9_-]+)`)
)

// OAuthTokenData is persisted to data/gemini_oauth.json.
type OAuthTokenData struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	ProjectID    string    `json:"project_id"`
	Email        string    `json:"email,omitempty"`
}

// GeminiOAuthFlow manages the entire OAuth lifecycle.
type GeminiOAuthFlow struct {
	mu          sync.Mutex
	tokenPath   string
	redirectURL string // e.g., http://localhost:8080/api/oauth/gemini/callback

	// In-flight OAuth state (single concurrent flow).
	state        string
	codeVerifier string
	clientID     string
	clientSecret string
}

// NewGeminiOAuthFlow creates a new flow manager.
func NewGeminiOAuthFlow(tokenPath, redirectURL string) *GeminiOAuthFlow {
	return &GeminiOAuthFlow{
		tokenPath:   tokenPath,
		redirectURL: redirectURL,
	}
}

// --- Gemini CLI Detection ---

// CLICredentials holds extracted OAuth client credentials.
type CLICredentials struct {
	ClientID     string
	ClientSecret string
}

// DetectGeminiCLI searches for an installed @google/gemini-cli and extracts
// OAuth Client ID + Secret from the bundled oauth2.js file.
func DetectGeminiCLI() (*CLICredentials, error) {
	// Check environment variable overrides first.
	if id, secret := envCredentials(); id != "" {
		return &CLICredentials{ClientID: id, ClientSecret: secret}, nil
	}

	geminiDir, err := findGeminiCLIDir()
	if err != nil {
		return nil, fmt.Errorf("gemini CLI not found: %w", err)
	}

	return extractCredentialsFromDir(geminiDir)
}

func envCredentials() (string, string) {
	for _, prefix := range []string{"OPENCLAW_GEMINI_OAUTH_", "GEMINI_CLI_OAUTH_"} {
		id := os.Getenv(prefix + "CLIENT_ID")
		secret := os.Getenv(prefix + "CLIENT_SECRET")
		if id != "" {
			return id, secret
		}
	}
	return "", ""
}

// findGeminiCLIDir locates the @google/gemini-cli package root.
func findGeminiCLIDir() (string, error) {
	// Resolve the `gemini` binary via PATH.
	binPath, err := exec.LookPath("gemini")
	if err != nil {
		return "", fmt.Errorf("gemini binary not in PATH: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		return "", fmt.Errorf("resolve symlink %s: %w", binPath, err)
	}

	// Go up two directory levels (from dist/index.js → package root).
	packageDir := filepath.Dir(filepath.Dir(resolved))

	if _, err := os.Stat(filepath.Join(packageDir, "package.json")); err != nil {
		return "", fmt.Errorf("no package.json at %s", packageDir)
	}

	return packageDir, nil
}

// extractCredentialsFromDir searches for oauth2.js in the Gemini CLI directory tree.
func extractCredentialsFromDir(geminiDir string) (*CLICredentials, error) {
	// Known paths within @google/gemini-cli.
	candidates := []string{
		filepath.Join(geminiDir, "node_modules", "@google", "gemini-cli-core", "dist", "src", "code_assist", "oauth2.js"),
		filepath.Join(geminiDir, "node_modules", "@google", "gemini-cli-core", "dist", "code_assist", "oauth2.js"),
	}

	for _, path := range candidates {
		creds, err := extractFromFile(path)
		if err == nil {
			return creds, nil
		}
	}

	// Fallback: walk directory tree up to depth 10.
	var found string
	_ = filepath.WalkDir(geminiDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
		// Limit depth.
		rel, _ := filepath.Rel(geminiDir, path)
		if strings.Count(rel, string(filepath.Separator)) > 10 {
			return filepath.SkipDir
		}
		if d.Name() == "oauth2.js" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})

	if found != "" {
		return extractFromFile(found)
	}

	return nil, errors.New("oauth2.js not found in Gemini CLI directory")
}

func extractFromFile(path string) (*CLICredentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	idMatch := clientIDRegex.FindString(content)
	secretMatch := clientSecretRegex.FindString(content)

	if idMatch == "" || secretMatch == "" {
		return nil, fmt.Errorf("credentials not found in %s", path)
	}

	return &CLICredentials{ClientID: idMatch, ClientSecret: secretMatch}, nil
}

// --- PKCE ---

func generatePKCE() (verifier, challenge string) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	verifier = hex.EncodeToString(buf)
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return
}

func generateState() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// --- OAuth Flow ---

// StartFlow generates an OAuth authorization URL and stores in-flight state.
func (f *GeminiOAuthFlow) StartFlow(clientID, clientSecret string) (authURL string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.clientID = clientID
	f.clientSecret = clientSecret
	f.state = generateState()
	f.codeVerifier, _ = generatePKCE()

	challenge := sha256.Sum256([]byte(f.codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(challenge[:])

	params := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {f.redirectURL},
		"scope":                 {strings.Join(oauthScopes, " ")},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {f.state},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}

	return oauthAuthURL + "?" + params.Encode(), nil
}

// HandleCallback processes the OAuth callback, exchanges code for tokens,
// discovers the project, and persists the result.
func (f *GeminiOAuthFlow) HandleCallback(ctx context.Context, code, state string) (*OAuthTokenData, error) {
	f.mu.Lock()
	expectedState := f.state
	verifier := f.codeVerifier
	clientID := f.clientID
	clientSecret := f.clientSecret
	// Clear in-flight state.
	f.state = ""
	f.codeVerifier = ""
	f.mu.Unlock()

	if expectedState == "" || state != expectedState {
		return nil, errors.New("invalid or expired OAuth state")
	}

	// Exchange authorization code for tokens.
	token, err := exchangeCode(ctx, clientID, clientSecret, code, verifier, f.redirectURL)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	// Fetch user email.
	email, _ := fetchUserEmail(ctx, token.AccessToken)

	// Discover project via Code Assist API.
	projectID, err := discoverProject(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("project discovery: %w", err)
	}

	data := &OAuthTokenData{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
		ProjectID:    projectID,
		Email:        email,
	}

	if err := saveTokenData(f.tokenPath, data); err != nil {
		log.Printf("WARNING: failed to persist OAuth token: %v", err)
	}

	return data, nil
}

// exchangeCode performs the OAuth2 token exchange with PKCE.
func exchangeCode(ctx context.Context, clientID, clientSecret, code, verifier, redirectURI string) (*oauth2.Token, error) {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURI,
	}

	return cfg.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
}

// --- User Info ---

func fetchUserEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthUserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	return info.Email, nil
}

// --- Code Assist Project Discovery ---

func codeAssistHeaders(accessToken string) map[string]string {
	return map[string]string{
		"Authorization":    "Bearer " + accessToken,
		"Content-Type":     "application/json",
		"User-Agent":       "google-api-nodejs-client/9.15.1",
		"X-Goog-Api-Client": "gl-node/koukeBOT",
	}
}

func discoverProject(ctx context.Context, accessToken string) (string, error) {
	headers := codeAssistHeaders(accessToken)

	// Try loadCodeAssist first.
	projectID, err := loadCodeAssist(ctx, headers)
	if err == nil && projectID != "" {
		return projectID, nil
	}
	log.Printf("loadCodeAssist: %v, trying onboardUser...", err)

	// Fall back to onboardUser.
	return onboardUser(ctx, headers)
}

func loadCodeAssist(ctx context.Context, headers map[string]string) (string, error) {
	body := `{"metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`

	respBody, err := doCodeAssistRequest(ctx, codeAssistURL+"/v1internal:loadCodeAssist", body, headers)
	if err != nil {
		return "", err
	}

	var result struct {
		CloudAICompanionProject any `json:"cloudaicompanionProject"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse loadCodeAssist response: %w", err)
	}

	return extractProjectID(result.CloudAICompanionProject), nil
}

func onboardUser(ctx context.Context, headers map[string]string) (string, error) {
	body := `{"tierId":"legacy-tier","metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`

	respBody, err := doCodeAssistRequest(ctx, codeAssistURL+"/v1internal:onboardUser", body, headers)
	if err != nil {
		return "", err
	}

	var lro struct {
		Done     bool   `json:"done"`
		Name     string `json:"name"`
		Response struct {
			CloudAICompanionProject struct {
				ID string `json:"id"`
			} `json:"cloudaicompanionProject"`
		} `json:"response"`
	}
	if err := json.Unmarshal(respBody, &lro); err != nil {
		return "", fmt.Errorf("parse onboardUser response: %w", err)
	}

	if lro.Done {
		return lro.Response.CloudAICompanionProject.ID, nil
	}

	if lro.Name == "" {
		return "", errors.New("onboardUser returned no operation name")
	}

	// Poll the long-running operation.
	return pollOperation(ctx, lro.Name, headers)
}

func pollOperation(ctx context.Context, operationName string, headers map[string]string) (string, error) {
	for attempt := 0; attempt < pollMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}

		reqURL := codeAssistURL + "/v1internal/" + operationName
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var lro struct {
			Done     bool `json:"done"`
			Response struct {
				CloudAICompanionProject struct {
					ID string `json:"id"`
				} `json:"cloudaicompanionProject"`
			} `json:"response"`
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err := json.Unmarshal(body, &lro); err != nil {
			continue
		}
		if lro.Done {
			return lro.Response.CloudAICompanionProject.ID, nil
		}
	}

	return "", errors.New("operation polling timeout")
}

func doCodeAssistRequest(ctx context.Context, url, body string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Code Assist API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// extractProjectID handles both string and object forms of cloudaicompanionProject.
func extractProjectID(v any) string {
	switch p := v.(type) {
	case string:
		return p
	case map[string]any:
		if id, ok := p["id"].(string); ok {
			return id
		}
	}
	return ""
}

// --- Token Persistence ---

// LoadStoredToken reads persisted OAuth token data.
// Returns nil if the file does not exist.
func LoadStoredToken(path string) (*OAuthTokenData, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var token OAuthTokenData
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	return &token, nil
}

func saveTokenData(path string, data *OAuthTokenData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// --- Token Refresh ---

// RefreshIfNeeded checks if the access token is expired and refreshes it.
// Updates the token data in place and persists to disk.
func RefreshIfNeeded(ctx context.Context, data *OAuthTokenData, tokenPath string) error {
	if time.Until(data.Expiry) > tokenExpiryMargin {
		return nil // Token still valid.
	}

	cfg := &oauth2.Config{
		ClientID:     data.ClientID,
		ClientSecret: data.ClientSecret,
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{
		RefreshToken: data.RefreshToken,
	}

	newToken, err := cfg.TokenSource(ctx, token).Token()
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}

	data.AccessToken = newToken.AccessToken
	data.TokenType = newToken.TokenType
	data.Expiry = newToken.Expiry
	if newToken.RefreshToken != "" {
		data.RefreshToken = newToken.RefreshToken
	}

	return saveTokenData(tokenPath, data)
}

// NewTokenSource creates an oauth2.TokenSource from stored token data.
// The returned source auto-refreshes using the client credentials.
func NewTokenSource(ctx context.Context, data *OAuthTokenData) oauth2.TokenSource {
	cfg := &oauth2.Config{
		ClientID:     data.ClientID,
		ClientSecret: data.ClientSecret,
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{
		AccessToken:  data.AccessToken,
		RefreshToken: data.RefreshToken,
		TokenType:    data.TokenType,
		Expiry:       data.Expiry,
	}

	return cfg.TokenSource(ctx, token)
}

// GetStatus returns the current OAuth status from the stored token file.
func (f *GeminiOAuthFlow) GetStatus() (*OAuthTokenData, error) {
	return LoadStoredToken(f.tokenPath)
}
