package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Provider type constants for explicit provider selection.
const (
	ProviderAPIKey = "api-key"
	ProviderOAuth  = "oauth"
)

// Settings holds the runtime-configurable values.
type Settings struct {
	GeminiAPIKey     string `json:"gemini_api_key"`
	GeminiModel      string `json:"gemini_model"`
	DiscordToken     string `json:"discord_token"`
	ProviderType     string `json:"provider_type"`        // "api-key", "oauth", or "" (auto)
	BraveAPIKey      string `json:"brave_api_key"`        // Brave Search API key for web search
	WebSearchEnabled bool   `json:"web_search_enabled"`   // Enable/disable web search tool
	DiscordUseReply  bool   `json:"discord_use_reply"`    // Use reply feature in Discord (default: false)
	PerplexityAPIKey string `json:"perplexity_api_key"`   // Perplexity API key for Sonar search (OpenClaw 相容)
	SearchProvider   string `json:"search_provider"`      // "brave" | "perplexity" | "auto" (OpenClaw 相容)
	WebFetchEnabled  bool   `json:"web_fetch_enabled"`    // Enable/disable web_fetch tool (OpenClaw 相容)

	// Time and location settings (OpenClaw 相容)
	Timezone   string `json:"timezone"`    // "Asia/Taipei", "America/New_York", "auto"
	TimeFormat string `json:"time_format"` // "auto", "12", "24"
	Location   string `json:"location"`    // Optional: explicit location "台北, 台灣"

	// Memory extraction settings
	MemoryExtractionEnabled bool   `json:"memory_extraction_enabled"` // Enable automatic post-response memory extraction
	MemoryExtractionModel   string `json:"memory_extraction_model"`   // Model for extraction (default: "gemini-2.0-flash")

	// Provider pool settings
	AutoRetryCount int `json:"auto_retry_count"` // Number of retries for short-delay 429s before switching accounts (default: 3)
}

// SettingsStore reads and writes settings to a JSON file.
type SettingsStore struct {
	mu   sync.RWMutex
	path string
}

func NewSettingsStore(path string) *SettingsStore {
	return &SettingsStore{path: path}
}

// Load reads settings from disk. Returns zero-value Settings if the file does not exist.
func (s *SettingsStore) Load() (Settings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// Save writes settings to disk, creating the parent directory if needed.
func (s *SettingsStore) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o600)
}
