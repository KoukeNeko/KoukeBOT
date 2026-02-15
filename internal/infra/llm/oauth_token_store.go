package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

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

// RefreshIfNeeded checks if the access token is expired and refreshes it.
func RefreshIfNeeded(ctx context.Context, data *OAuthTokenData, tokenPath string) error {
	if time.Until(data.Expiry) > tokenExpiryMargin {
		return nil
	}

	cfg := &oauth2.Config{
		ClientID:     data.ClientID,
		ClientSecret: data.ClientSecret,
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{RefreshToken: data.RefreshToken}
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

	if tokenPath == "" {
		return nil
	}
	return saveTokenData(tokenPath, data)
}

// NewTokenSource creates an oauth2.TokenSource from stored token data.
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
