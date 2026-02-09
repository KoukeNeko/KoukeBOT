package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoadOAuthPool reads the pool of OAuth tokens from disk.
// Returns nil (not error) if the file doesn't exist.
func LoadOAuthPool(path string) ([]*OAuthTokenData, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pool file: %w", err)
	}

	var tokens []*OAuthTokenData
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse pool file: %w", err)
	}
	return tokens, nil
}

// SaveOAuthPool writes the pool of OAuth tokens to disk.
func SaveOAuthPool(path string, tokens []*OAuthTokenData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// AddToOAuthPool adds or replaces a token in the pool file (by email).
func AddToOAuthPool(path string, token *OAuthTokenData) error {
	tokens, err := LoadOAuthPool(path)
	if err != nil {
		return err
	}

	// Replace existing entry with same email, or append
	found := false
	for i, t := range tokens {
		if t.Email == token.Email {
			tokens[i] = token
			found = true
			break
		}
	}
	if !found {
		tokens = append(tokens, token)
	}

	return SaveOAuthPool(path, tokens)
}

// RemoveFromOAuthPool removes a token by email from the pool file.
func RemoveFromOAuthPool(path string, email string) error {
	tokens, err := LoadOAuthPool(path)
	if err != nil {
		return err
	}

	filtered := make([]*OAuthTokenData, 0, len(tokens))
	for _, t := range tokens {
		if t.Email != email {
			filtered = append(filtered, t)
		}
	}

	return SaveOAuthPool(path, filtered)
}

// MigrateSingleTokenToPool checks if a single-token file exists but a pool file doesn't,
// and migrates the single token into the pool.
func MigrateSingleTokenToPool(singlePath, poolPath string) {
	// Skip if pool already exists
	if _, err := os.Stat(poolPath); err == nil {
		return
	}

	token, err := LoadStoredToken(singlePath)
	if err != nil || token == nil {
		return
	}

	_ = SaveOAuthPool(poolPath, []*OAuthTokenData{token})
}
