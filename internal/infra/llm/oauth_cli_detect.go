package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CLICredentials holds extracted OAuth client credentials.
type CLICredentials struct {
	ClientID     string
	ClientSecret string
}

// DetectGeminiCLI searches for an installed @google/gemini-cli and extracts
// OAuth Client ID + Secret from the bundled oauth2.js file.
func DetectGeminiCLI() (*CLICredentials, error) {
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

func findGeminiCLIDir() (string, error) {
	binPath, err := exec.LookPath("gemini")
	if err != nil {
		return "", fmt.Errorf("gemini binary not in PATH: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		return "", fmt.Errorf("resolve symlink %s: %w", binPath, err)
	}

	packageDir := filepath.Dir(filepath.Dir(resolved))
	if _, err := os.Stat(filepath.Join(packageDir, "package.json")); err != nil {
		return "", fmt.Errorf("no package.json at %s", packageDir)
	}

	return packageDir, nil
}

func extractCredentialsFromDir(geminiDir string) (*CLICredentials, error) {
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

	var found string
	_ = filepath.WalkDir(geminiDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
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

const geminiCLICredsFile = ".gemini/oauth_creds.json"

type geminiCLICreds struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiryDate   int64  `json:"expiry_date"`
}

// DetectGeminiCLIToken reads the stored OAuth token from Gemini CLI (~/.gemini/oauth_creds.json)
// and combines it with client credentials extracted from the CLI bundle.
func DetectGeminiCLIToken(ctx context.Context) (*OAuthTokenData, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	credsPath := filepath.Join(home, geminiCLICredsFile)
	raw, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", credsPath, err)
	}

	var creds geminiCLICreds
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", credsPath, err)
	}
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("%s has no refresh_token", credsPath)
	}

	cli, err := DetectGeminiCLI()
	if err != nil {
		return nil, fmt.Errorf("cannot extract client credentials: %w", err)
	}

	expiry := time.Time{}
	if creds.ExpiryDate > 0 {
		expiry = time.UnixMilli(creds.ExpiryDate)
	}

	data := &OAuthTokenData{
		ClientID:     cli.ClientID,
		ClientSecret: cli.ClientSecret,
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		TokenType:    creds.TokenType,
		Expiry:       expiry,
		ProviderID:   ProviderGoogleGeminiCLI,
	}

	if err := RefreshIfNeeded(ctx, data, ""); err != nil {
		logFlow.Warnf("token refresh during import: %v (continuing with existing token)", err)
	}
	if email, err := fetchUserEmail(ctx, data.AccessToken); err == nil {
		data.Email = email
	} else {
		logFlow.Warnf("failed to fetch email during import: %v", err)
	}
	if projectID, tierID, endpointUsed, err := discoverProjectWithProvider(
		ctx,
		data.AccessToken,
		data.ProviderID,
		EndpointPolicyPrimaryThenSandbox,
	); err == nil {
		data.ProjectID = projectID
		data.TierID = tierID
		data.EndpointUsed = endpointUsed
	} else {
		logFlow.Warnf("failed to discover project during import: %v", err)
	}

	return data, nil
}

// ImportTokenFromJSON imports a token from raw JSON (e.g. pasted ~/.gemini/oauth_creds.json content).
// clientID/clientSecret can be provided; if empty, they are extracted from the Gemini CLI bundle.
func (f *GeminiOAuthFlow) ImportTokenFromJSON(ctx context.Context, raw []byte, clientID, clientSecret string) (*OAuthTokenData, error) {
	var creds geminiCLICreds
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if creds.RefreshToken == "" {
		return nil, errors.New("JSON has no refresh_token")
	}

	if clientID == "" || clientSecret == "" {
		cli, err := DetectGeminiCLI()
		if err != nil {
			return nil, fmt.Errorf("no client credentials provided and cannot extract from Gemini CLI: %w", err)
		}
		clientID = cli.ClientID
		clientSecret = cli.ClientSecret
	}

	expiry := time.Time{}
	if creds.ExpiryDate > 0 {
		expiry = time.UnixMilli(creds.ExpiryDate)
	}

	data := &OAuthTokenData{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		TokenType:    creds.TokenType,
		Expiry:       expiry,
		ProviderID:   ProviderGoogleGeminiCLI,
	}

	if err := RefreshIfNeeded(ctx, data, ""); err != nil {
		logFlow.Warnf("token refresh during paste import: %v (continuing)", err)
	}
	if email, err := fetchUserEmail(ctx, data.AccessToken); err == nil {
		data.Email = email
	} else {
		logFlow.Warnf("failed to fetch email during paste import: %v", err)
	}
	if projectID, tierID, endpointUsed, err := discoverProjectWithProvider(
		ctx,
		data.AccessToken,
		data.ProviderID,
		EndpointPolicyPrimaryThenSandbox,
	); err == nil {
		data.ProjectID = projectID
		data.TierID = tierID
		data.EndpointUsed = endpointUsed
	} else {
		logFlow.Warnf("failed to discover project during paste import: %v", err)
	}

	if err := saveTokenData(f.tokenPath, data); err != nil {
		logFlow.Warnf("failed to persist pasted token: %v", err)
	}
	if f.poolPath != "" {
		if err := AddToOAuthPool(f.poolPath, data); err != nil {
			logFlow.Warnf("failed to update pool file: %v", err)
		}
	}

	return data, nil
}

// ImportGeminiCLIToken reads the stored Gemini CLI token, persists it, and adds to pool.
func (f *GeminiOAuthFlow) ImportGeminiCLIToken(ctx context.Context) (*OAuthTokenData, error) {
	data, err := DetectGeminiCLIToken(ctx)
	if err != nil {
		return nil, err
	}

	if err := saveTokenData(f.tokenPath, data); err != nil {
		logFlow.Warnf("failed to persist imported token: %v", err)
	}
	if f.poolPath != "" {
		if err := AddToOAuthPool(f.poolPath, data); err != nil {
			logFlow.Warnf("failed to update pool file: %v", err)
		}
	}

	return data, nil
}
