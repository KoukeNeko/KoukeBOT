package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Provider type constants for explicit provider selection.
const (
	ProviderAPIKey = "api-key"
	ProviderOAuth  = "oauth"

	DefaultPersonaLockEveryTurns      = 4
	DefaultAnalysisVisibility         = "visible"
	DefaultNegativeConstraintPrompt   = "You are FORBIDDEN from switching into a generic assistant tone. Keep persona-specific stance, diction, and attitude."
	DefaultDiscordObserveMaxResults   = 25
	DiscordObserveScopeCurrentChannel = "current_channel"
	DiscordObserveScopeCurrentGuild   = "current_guild"
	DefaultDiscordObserveMessageScope = DiscordObserveScopeCurrentGuild
	DefaultMaxToolRounds              = 3
	DefaultMaxWebSearchCallsPerTurn   = 2
	DefaultTransientRecoveryWaitMax   = 5
)

// Settings holds the runtime-configurable values.
type Settings struct {
	GeminiAPIKey     string `json:"gemini_api_key"`
	GeminiModel      string `json:"gemini_model"`
	DiscordToken     string `json:"discord_token"`
	ProviderType     string `json:"provider_type"`      // "api-key", "oauth", or "" (auto)
	BraveAPIKey      string `json:"brave_api_key"`      // Brave Search API key for web search
	WebSearchEnabled bool   `json:"web_search_enabled"` // Enable/disable web search tool
	DiscordUseReply  bool   `json:"discord_use_reply"`  // Use reply feature in Discord (default: false)
	PerplexityAPIKey string `json:"perplexity_api_key"` // Perplexity API key for Sonar search (OpenClaw 相容)
	SearchProvider   string `json:"search_provider"`    // "brave" | "perplexity" | "auto" (OpenClaw 相容)
	WebFetchEnabled  bool   `json:"web_fetch_enabled"`  // Enable/disable web_fetch tool (OpenClaw 相容)

	// Time and location settings (OpenClaw 相容)
	Timezone   string `json:"timezone"`    // "Asia/Taipei", "America/New_York", "auto"
	TimeFormat string `json:"time_format"` // "auto", "12", "24"
	Location   string `json:"location"`    // Optional: explicit location "台北, 台灣"

	// Context management settings (OpenClaw-style memory flush)
	ContextFlushEnabled   bool `json:"context_flush_enabled"`   // Enable pre-compaction memory flush (default: true)
	ContextFlushThreshold int  `json:"context_flush_threshold"` // Tokens before limit to trigger flush (default: 4000)
	ContextWindowSize     int  `json:"context_window_size"`     // Effective context window size (default: 800000)

	// Memory extraction settings (UI-visible, not yet wired to backend logic)
	MemoryExtractionEnabled bool   `json:"memory_extraction_enabled"` // Enable automatic memory extraction after conversations
	MemoryExtractionModel   string `json:"memory_extraction_model"`   // Model for memory extraction (e.g. "gemini-2.0-flash")

	// Provider pool settings
	AutoRetryCount int `json:"auto_retry_count"` // Number of retries for short-delay 429s before switching accounts (default: 3)

	// Tool loop guard / resilience settings
	MaxToolRounds                   int  `json:"max_tool_rounds"`
	MaxWebSearchCallsPerTurn        int  `json:"max_web_search_calls_per_turn"`
	ForceFinalAnswerAfterToolLimit  bool `json:"force_final_answer_after_tool_limit"`
	TransientRecoveryWaitEnabled    bool `json:"transient_recovery_wait_enabled"`
	TransientRecoveryWaitMaxSeconds int  `json:"transient_recovery_wait_max_seconds"`
	DiscordShowInternalToolErrors   bool `json:"discord_show_internal_tool_errors"`

	// Discord logging settings
	LogChannelID string `json:"log_channel_id"` // Discord channel ID for stdout logs (optional)
	// Preferred active persona for Discord bot runtime.
	DiscordPersonaID string `json:"discord_persona_id"`

	// Discord observe tool settings (read-only Discord inspection)
	DiscordObserveEnabled        bool   `json:"discord_observe_enabled"`
	DiscordMemberIntentEnabled   bool   `json:"discord_member_intent_enabled"`
	DiscordPresenceIntentEnabled bool   `json:"discord_presence_intent_enabled"`
	DiscordObserveMaxResults     int    `json:"discord_observe_max_results"`
	DiscordObserveMessageScope   string `json:"discord_observe_message_scope"` // "current_channel" | "current_guild"

	// Prompt guard / persona stability settings
	PersonaLockEveryTurns     int    `json:"persona_lock_every_turns"`    // Inject persona lock every N user turns (default: 4)
	TripletOutputEnabled      bool   `json:"triplet_output_enabled"`      // Enforce [Status]/[Analysis]/[Response] format
	AnalysisVisibility        string `json:"analysis_visibility"`         // "visible" | "hidden" | "debug"
	NegativeConstraintEnabled bool   `json:"negative_constraint_enabled"` // Enable anti-generic constraints
	NegativeConstraintDefault string `json:"negative_constraint_default"` // Global fallback negative constraint
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
		return normalizeSettings(Settings{}, map[string]json.RawMessage{}), nil
	}
	if err != nil {
		return Settings{}, err
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Settings{}, err
	}
	settings.ProviderType = normalizeProviderType(settings.ProviderType)
	settings = normalizeSettings(settings, raw)
	return settings, nil
}

// Save writes settings to disk, creating the parent directory if needed.
func (s *SettingsStore) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings.ProviderType = normalizeProviderType(settings.ProviderType)
	settings = normalizeSettings(settings, nil)

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o600)
}

func normalizeProviderType(providerType string) string {
	switch providerType {
	case "", ProviderAPIKey, ProviderOAuth:
		return providerType
	case "cli":
		return ProviderOAuth
	default:
		// Backward compatibility: legacy non-empty values map to OAuth.
		return ProviderOAuth
	}
}

func normalizeSettings(settings Settings, raw map[string]json.RawMessage) Settings {
	if settings.PersonaLockEveryTurns <= 0 {
		settings.PersonaLockEveryTurns = DefaultPersonaLockEveryTurns
	}

	if raw == nil {
		// Save path or programmatic assignment: preserve explicit false values.
		if settings.AnalysisVisibility == "" {
			settings.AnalysisVisibility = DefaultAnalysisVisibility
		}
		if strings.TrimSpace(settings.NegativeConstraintDefault) == "" {
			settings.NegativeConstraintDefault = DefaultNegativeConstraintPrompt
		}
		if settings.DiscordObserveMaxResults <= 0 {
			settings.DiscordObserveMaxResults = DefaultDiscordObserveMaxResults
		}
		if settings.DiscordObserveMessageScope == "" {
			settings.DiscordObserveMessageScope = DefaultDiscordObserveMessageScope
		}
		if settings.MaxToolRounds <= 0 {
			settings.MaxToolRounds = DefaultMaxToolRounds
		}
		if settings.MaxWebSearchCallsPerTurn <= 0 {
			settings.MaxWebSearchCallsPerTurn = DefaultMaxWebSearchCallsPerTurn
		}
		if settings.TransientRecoveryWaitMaxSeconds <= 0 {
			settings.TransientRecoveryWaitMaxSeconds = DefaultTransientRecoveryWaitMax
		}
		return normalizeVisibilityAndBooleans(settings)
	}

	// Load path: if key missing in legacy settings.json, apply defaults.
	if _, ok := raw["triplet_output_enabled"]; !ok {
		settings.TripletOutputEnabled = true
	}
	if _, ok := raw["negative_constraint_enabled"]; !ok {
		settings.NegativeConstraintEnabled = true
	}
	if _, ok := raw["analysis_visibility"]; !ok {
		settings.AnalysisVisibility = DefaultAnalysisVisibility
	}
	if _, ok := raw["negative_constraint_default"]; !ok {
		settings.NegativeConstraintDefault = DefaultNegativeConstraintPrompt
	}
	if _, ok := raw["persona_lock_every_turns"]; !ok && settings.PersonaLockEveryTurns <= 0 {
		settings.PersonaLockEveryTurns = DefaultPersonaLockEveryTurns
	}
	if _, ok := raw["discord_observe_max_results"]; !ok && settings.DiscordObserveMaxResults <= 0 {
		settings.DiscordObserveMaxResults = DefaultDiscordObserveMaxResults
	}
	if _, ok := raw["discord_observe_message_scope"]; !ok && settings.DiscordObserveMessageScope == "" {
		settings.DiscordObserveMessageScope = DefaultDiscordObserveMessageScope
	}
	if _, ok := raw["max_tool_rounds"]; !ok {
		settings.MaxToolRounds = DefaultMaxToolRounds
	}
	if _, ok := raw["max_web_search_calls_per_turn"]; !ok {
		settings.MaxWebSearchCallsPerTurn = DefaultMaxWebSearchCallsPerTurn
	}
	if _, ok := raw["force_final_answer_after_tool_limit"]; !ok {
		settings.ForceFinalAnswerAfterToolLimit = true
	}
	if _, ok := raw["transient_recovery_wait_enabled"]; !ok {
		settings.TransientRecoveryWaitEnabled = true
	}
	if _, ok := raw["transient_recovery_wait_max_seconds"]; !ok {
		settings.TransientRecoveryWaitMaxSeconds = DefaultTransientRecoveryWaitMax
	}
	if _, ok := raw["discord_show_internal_tool_errors"]; !ok {
		settings.DiscordShowInternalToolErrors = true
	}

	if strings.TrimSpace(settings.NegativeConstraintDefault) == "" {
		settings.NegativeConstraintDefault = DefaultNegativeConstraintPrompt
	}

	return normalizeVisibilityAndBooleans(settings)
}

func normalizeVisibilityAndBooleans(settings Settings) Settings {
	switch settings.AnalysisVisibility {
	case "visible", "hidden", "debug":
		// keep
	default:
		settings.AnalysisVisibility = DefaultAnalysisVisibility
	}
	if settings.PersonaLockEveryTurns <= 0 {
		settings.PersonaLockEveryTurns = DefaultPersonaLockEveryTurns
	}
	if strings.TrimSpace(settings.NegativeConstraintDefault) == "" {
		settings.NegativeConstraintDefault = DefaultNegativeConstraintPrompt
	}
	if settings.DiscordObserveMaxResults <= 0 {
		settings.DiscordObserveMaxResults = DefaultDiscordObserveMaxResults
	}
	if settings.DiscordObserveMaxResults > 100 {
		settings.DiscordObserveMaxResults = 100
	}
	switch settings.DiscordObserveMessageScope {
	case DiscordObserveScopeCurrentChannel, DiscordObserveScopeCurrentGuild:
		// valid
	default:
		settings.DiscordObserveMessageScope = DefaultDiscordObserveMessageScope
	}
	if settings.MaxToolRounds < 1 {
		settings.MaxToolRounds = DefaultMaxToolRounds
	}
	if settings.MaxToolRounds > 6 {
		settings.MaxToolRounds = 6
	}
	if settings.MaxWebSearchCallsPerTurn < 1 {
		settings.MaxWebSearchCallsPerTurn = DefaultMaxWebSearchCallsPerTurn
	}
	if settings.MaxWebSearchCallsPerTurn > 4 {
		settings.MaxWebSearchCallsPerTurn = 4
	}
	if settings.TransientRecoveryWaitMaxSeconds < 1 {
		settings.TransientRecoveryWaitMaxSeconds = DefaultTransientRecoveryWaitMax
	}
	if settings.TransientRecoveryWaitMaxSeconds > 15 {
		settings.TransientRecoveryWaitMaxSeconds = 15
	}
	return settings
}
