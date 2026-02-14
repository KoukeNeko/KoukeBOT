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
			merged := cloneOAuthTokenData(token)
			if merged.UsageStats == nil {
				merged.UsageStats = cloneOAuthUsageStats(t.UsageStats)
			}
			tokens[i] = merged
			found = true
			break
		}
	}
	if !found {
		tokens = append(tokens, cloneOAuthTokenData(token))
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

// UpdateOAuthPoolToken updates an existing token entry by email.
// Returns an error if the email does not exist in the pool.
func UpdateOAuthPoolToken(path string, email string, token *OAuthTokenData) error {
	tokens, err := LoadOAuthPool(path)
	if err != nil {
		return err
	}

	for i, t := range tokens {
		if t.Email == email {
			merged := cloneOAuthTokenData(token)
			// Refresh writes should preserve existing usage stats.
			if merged.UsageStats == nil {
				merged.UsageStats = cloneOAuthUsageStats(t.UsageStats)
			}
			tokens[i] = merged
			return SaveOAuthPool(path, tokens)
		}
	}

	return fmt.Errorf("email not found in OAuth pool: %s", email)
}

// UpdateOAuthPoolUsageStats updates usage stats for one account entry.
func UpdateOAuthPoolUsageStats(path string, email string, usage *OAuthUsageStats) error {
	tokens, err := LoadOAuthPool(path)
	if err != nil {
		return err
	}

	for i, t := range tokens {
		if t.Email == email {
			tokens[i].UsageStats = cloneOAuthUsageStats(usage)
			return SaveOAuthPool(path, tokens)
		}
	}
	return fmt.Errorf("email not found in OAuth pool: %s", email)
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

func cloneOAuthTokenData(src *OAuthTokenData) *OAuthTokenData {
	if src == nil {
		return nil
	}
	out := *src
	out.UsageStats = cloneOAuthUsageStats(src.UsageStats)
	return &out
}

func cloneOAuthUsageStats(src *OAuthUsageStats) *OAuthUsageStats {
	if src == nil {
		return nil
	}
	out := *src
	if len(src.HourlyBuckets) > 0 {
		out.HourlyBuckets = append([]OAuthUsageBucket(nil), src.HourlyBuckets...)
	}
	return &out
}
