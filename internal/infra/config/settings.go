package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Provider type constants for explicit provider selection.
const (
	ProviderAPIKey = "api-key"
	ProviderOAuth  = "oauth"

	WebToolModeClassic       = "classic"
	WebToolModePlaywrightMCP = "playwright_mcp"

	ManagedPlaywrightMCPServerID = "playwright_mcp"

	DefaultPersonaLockEveryTurns      = 4
	DefaultAnalysisVisibility         = "visible"
	DefaultNegativeConstraintPrompt   = "You are FORBIDDEN from switching into a generic assistant tone. Keep persona-specific stance, diction, and attitude."
	DefaultDiscordObserveMaxResults   = 25
	DiscordObserveScopeCurrentChannel = "current_channel"
	DiscordObserveScopeCurrentGuild   = "current_guild"
	DefaultDiscordObserveMessageScope = DiscordObserveScopeCurrentGuild
	DefaultToolGuardEnabled           = true
	DefaultMaxToolRounds              = 5
	DefaultMaxWebSearchCallsPerTurn   = 2
	DefaultTransientRecoveryWaitMax   = 60
	MaxTransientRecoveryWaitMax       = 120
	DefaultMemoryRecentRounds         = 50
	MinMemoryRecentRounds             = 20
	MaxMemoryRecentRounds             = 100
	DefaultScheduledMonitorMaxPerUser = 3
	MaxScheduledMonitorMaxPerUser     = 20
	DefaultRequestQueueCapacity       = 10
	DefaultRequestQueueTimeoutS       = 120
	MinRequestQueueCapacity           = 1
	MaxRequestQueueCapacity           = 50
	MinRequestQueueTimeoutS           = 10
	MaxRequestQueueTimeoutS           = 300
	DefaultMCPDiscoveryTimeoutSeconds = 10
	DefaultMCPToolCallTimeoutSeconds  = 20
	MinMCPDiscoveryTimeoutSeconds     = 1
	MaxMCPDiscoveryTimeoutSeconds     = 30
	MinMCPToolCallTimeoutSeconds      = 1
	MaxMCPToolCallTimeoutSeconds      = 120
)

// Settings holds the runtime-configurable values.
type Settings struct {
	GeminiAPIKey     string `json:"gemini_api_key"`
	AnthropicAPIKey  string `json:"anthropic_api_key"`
	GeminiModel      string `json:"gemini_model"`
	DiscordToken     string `json:"discord_token"`
	ProviderType     string `json:"provider_type"`      // "api-key", "oauth", or "" (auto)
	ProviderID       string `json:"provider_id"`        // "google-gemini-cli" | "google-antigravity" | "gemini-api-key" | "openai-api-key" | "aistudio-rpc" | "anthropic-api-key" | "anthropic-setup-token"
	ModelID          string `json:"model_id"`           // "provider/model"
	FallbackEnabled  bool   `json:"fallback_enabled"`   // OpenClaw-style fallback switch
	FallbackModelID  string `json:"fallback_model_id"`  // "provider/model"
	EndpointPolicy   string `json:"endpoint_policy"`    // "primary_then_sandbox"
	BraveAPIKey      string `json:"brave_api_key"`      // Brave Search API key for web search
	WebSearchEnabled bool   `json:"web_search_enabled"` // Enable/disable web search tool
	DiscordUseReply  bool   `json:"discord_use_reply"`  // Use reply feature in Discord (default: false)
	// Whether to strip leading bot mention tokens (<@bot>, <@!bot>) from inbound Discord text.
	// Default false (preserve mention text).
	DiscordStripLeadingBotMention bool   `json:"discord_strip_leading_bot_mention"`
	PerplexityAPIKey              string `json:"perplexity_api_key"` // Perplexity API key for Sonar search (OpenClaw 相容)
	SearchProvider                string `json:"search_provider"`    // "brave" | "perplexity" | "auto" (OpenClaw 相容)
	WebFetchEnabled               bool   `json:"web_fetch_enabled"`  // Enable/disable web_fetch tool (OpenClaw 相容)
	WebToolMode                   string `json:"web_tool_mode"`      // "classic" | "playwright_mcp"

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
	MemoryRecentRounds      int    `json:"memory_recent_rounds"`      // Number of recent user+assistant rounds to keep as raw history

	// Provider pool settings
	AutoRetryCount int `json:"auto_retry_count"` // Number of retries for short-delay 429s before switching accounts (default: 3)

	// Fallback model when primary has no capacity (e.g. "gemini-2.0-flash")
	FallbackModelEnabled  bool   `json:"fallback_model_enabled"` // default: false
	FallbackModel         string `json:"fallback_model"`
	FallbackSecondModelID string `json:"fallback_second_model_id"` // "provider/model"
	FallbackSecondModel   string `json:"fallback_second_model"`
	FallbackThirdModelID  string `json:"fallback_third_model_id"` // "provider/model"
	FallbackThirdModel    string `json:"fallback_third_model"`

	// Request queue settings
	RequestQueueEnabled  bool `json:"request_queue_enabled"`
	RequestQueueCapacity int  `json:"request_queue_capacity"`        // max queued requests (default: 10)
	RequestQueueTimeoutS int  `json:"request_queue_timeout_seconds"` // max wait in seconds (default: 120)

	// Circuit breaker settings
	CircuitBreakerEnabled          bool `json:"circuit_breaker_enabled"`
	CircuitBreakerFailureThreshold int  `json:"circuit_breaker_failure_threshold"`     // default: 5
	CircuitBreakerOpenDurationS    int  `json:"circuit_breaker_open_duration_seconds"` // default: 60

	// Tool loop guard / resilience settings
	ToolGuardEnabled                bool `json:"tool_guard_enabled"`
	MaxToolRounds                   int  `json:"max_tool_rounds"`
	MaxWebSearchCallsPerTurn        int  `json:"max_web_search_calls_per_turn"`
	ForceFinalAnswerAfterToolLimit  bool `json:"force_final_answer_after_tool_limit"`
	TransientRecoveryWaitEnabled    bool `json:"transient_recovery_wait_enabled"`
	TransientRecoveryWaitMaxSeconds int  `json:"transient_recovery_wait_max_seconds"`
	DiscordShowInternalToolErrors   bool `json:"discord_show_internal_tool_errors"`
	ScheduledMonitorMaxPerUser      int  `json:"scheduled_monitor_max_per_user"`

	// MCP settings
	MCP MCPSettings `json:"mcp"`

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

type MCPSettings struct {
	Enabled                 bool               `json:"enabled"`
	VerboseLogging          bool               `json:"verbose_logging"`
	DiscoveryTimeoutSeconds int                `json:"discovery_timeout_seconds"`
	ToolCallTimeoutSeconds  int                `json:"tool_call_timeout_seconds"`
	Servers                 []MCPServerSetting `json:"servers"`
}

type MCPServerSetting struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Enabled       bool              `json:"enabled"`
	Transport     string            `json:"transport"` // "stdio" | "http" | "sse"
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	ToolAllowlist []string          `json:"tool_allowlist"`
	Instructions  string            `json:"instructions"` // per-server usage guidelines injected into system prompt
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
	s.mu.Lock()
	defer s.mu.Unlock()

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
	needsMigration := shouldMigrateLegacySettings(raw)
	settings.ProviderType = normalizeProviderType(settings.ProviderType)
	settings = normalizeSettings(settings, raw)

	if needsMigration {
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err == nil {
			backupPath := fmt.Sprintf("%s.migration_backup.%s.json", s.path, time.Now().UTC().Format("20060102T150405Z"))
			if writeErr := os.WriteFile(backupPath, data, 0o600); writeErr != nil {
				// Best effort only; continue with migrated settings.
			}
			if normalized, marshalErr := json.MarshalIndent(settings, "", "  "); marshalErr == nil {
				_ = os.WriteFile(s.path, normalized, 0o600)
			}
		}
	}
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

func normalizeWebToolMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case WebToolModeClassic:
		return WebToolModeClassic
	case WebToolModePlaywrightMCP:
		return WebToolModePlaywrightMCP
	default:
		return WebToolModeClassic
	}
}

func NormalizeWebToolMode(mode string) string {
	return normalizeWebToolMode(mode)
}

func normalizeSettings(settings Settings, raw map[string]json.RawMessage) Settings {
	settings.WebToolMode = normalizeWebToolMode(settings.WebToolMode)
	settings.EndpointPolicy = normalizeEndpointPolicyForSettings(settings.EndpointPolicy)

	settings.ProviderID, settings.ModelID, settings.GeminiModel = resolveProviderModelForSettings(
		settings.ProviderID,
		settings.ModelID,
		settings.ProviderType,
		settings.GeminiModel,
		settings.GeminiAPIKey,
		settings.AnthropicAPIKey,
	)
	settings.ProviderType = syncLegacyProviderTypeForSettings(settings.ProviderType, settings.ProviderID, settings.GeminiAPIKey, settings.AnthropicAPIKey)

	if settings.FallbackEnabled ||
		settings.FallbackModelEnabled ||
		settings.FallbackModelID != "" ||
		settings.FallbackModel != "" ||
		settings.FallbackSecondModelID != "" ||
		settings.FallbackSecondModel != "" ||
		settings.FallbackThirdModelID != "" ||
		settings.FallbackThirdModel != "" {
		settings.FallbackEnabled = settings.FallbackEnabled || settings.FallbackModelEnabled
	}
	normalizeFallbackChainForSettings(&settings, settings.ProviderID, settings.GeminiModel)
	settings.FallbackModelEnabled = settings.FallbackEnabled

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
		if settings.MemoryRecentRounds <= 0 {
			settings.MemoryRecentRounds = DefaultMemoryRecentRounds
		}
		if settings.ScheduledMonitorMaxPerUser <= 0 {
			settings.ScheduledMonitorMaxPerUser = DefaultScheduledMonitorMaxPerUser
		}
		if settings.RequestQueueCapacity <= 0 {
			settings.RequestQueueCapacity = DefaultRequestQueueCapacity
		}
		if settings.RequestQueueTimeoutS <= 0 {
			settings.RequestQueueTimeoutS = DefaultRequestQueueTimeoutS
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
	if _, ok := raw["tool_guard_enabled"]; !ok {
		settings.ToolGuardEnabled = DefaultToolGuardEnabled
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
	if _, ok := raw["discord_strip_leading_bot_mention"]; !ok {
		settings.DiscordStripLeadingBotMention = false
	}
	if _, ok := raw["memory_recent_rounds"]; !ok {
		settings.MemoryRecentRounds = DefaultMemoryRecentRounds
	}
	if _, ok := raw["request_queue_enabled"]; !ok {
		settings.RequestQueueEnabled = true
	}
	if _, ok := raw["request_queue_capacity"]; !ok {
		settings.RequestQueueCapacity = DefaultRequestQueueCapacity
	}
	if _, ok := raw["request_queue_timeout_seconds"]; !ok {
		settings.RequestQueueTimeoutS = DefaultRequestQueueTimeoutS
	}
	if _, ok := raw["scheduled_monitor_max_per_user"]; !ok {
		settings.ScheduledMonitorMaxPerUser = DefaultScheduledMonitorMaxPerUser
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
	if settings.TransientRecoveryWaitMaxSeconds > MaxTransientRecoveryWaitMax {
		settings.TransientRecoveryWaitMaxSeconds = MaxTransientRecoveryWaitMax
	}
	if settings.MemoryRecentRounds < MinMemoryRecentRounds {
		settings.MemoryRecentRounds = DefaultMemoryRecentRounds
	}
	if settings.MemoryRecentRounds > MaxMemoryRecentRounds {
		settings.MemoryRecentRounds = MaxMemoryRecentRounds
	}
	if settings.ScheduledMonitorMaxPerUser < 1 {
		settings.ScheduledMonitorMaxPerUser = DefaultScheduledMonitorMaxPerUser
	}
	if settings.ScheduledMonitorMaxPerUser > MaxScheduledMonitorMaxPerUser {
		settings.ScheduledMonitorMaxPerUser = MaxScheduledMonitorMaxPerUser
	}
	if settings.RequestQueueCapacity < MinRequestQueueCapacity {
		settings.RequestQueueCapacity = DefaultRequestQueueCapacity
	}
	if settings.RequestQueueCapacity > MaxRequestQueueCapacity {
		settings.RequestQueueCapacity = MaxRequestQueueCapacity
	}
	if settings.RequestQueueTimeoutS < MinRequestQueueTimeoutS {
		settings.RequestQueueTimeoutS = DefaultRequestQueueTimeoutS
	}
	if settings.RequestQueueTimeoutS > MaxRequestQueueTimeoutS {
		settings.RequestQueueTimeoutS = MaxRequestQueueTimeoutS
	}
	settings.MCP = normalizeMCPSettings(settings.MCP)
	return settings
}

func shouldMigrateLegacySettings(raw map[string]json.RawMessage) bool {
	if raw == nil {
		return false
	}
	_, hasProviderID := raw["provider_id"]
	_, hasModelID := raw["model_id"]
	if hasProviderID {
		if !hasModelID {
			return true
		}
		return false
	}
	_, hasLegacyProvider := raw["provider_type"]
	_, hasLegacyModel := raw["gemini_model"]
	return hasLegacyProvider || hasLegacyModel
}

func resolveProviderModelForSettings(providerID, modelID, providerType, legacyModel, geminiAPIKey, anthropicAPIKey string) (string, string, string) {
	pid := normalizeGoogleProviderIDForSettings(providerID)
	if pid == "" {
		switch normalizeProviderType(providerType) {
		case ProviderAPIKey:
			if strings.TrimSpace(geminiAPIKey) != "" {
				pid = "gemini-api-key"
			} else if strings.TrimSpace(anthropicAPIKey) != "" {
				pid = "anthropic-api-key"
			} else {
				pid = "gemini-api-key"
			}
		case ProviderOAuth:
			pid = "google-gemini-cli"
		default:
			if strings.TrimSpace(geminiAPIKey) != "" {
				pid = "gemini-api-key"
			} else if strings.TrimSpace(anthropicAPIKey) != "" {
				pid = "anthropic-api-key"
			} else {
				pid = "google-gemini-cli"
			}
		}
	}

	if mProvider, mModel := splitProviderModelIDForSettings(modelID); mModel != "" {
		if mProvider != "" {
			pid = mProvider
		}
		normalized := normalizeModelAliasForProviderForSettings(pid, mModel)
		return pid, buildProviderModelIDForSettings(pid, normalized), normalized
	}

	normalizedLegacyModel := normalizeModelAliasForProviderForSettings(pid, legacyModel)
	if normalizedLegacyModel == "" {
		switch pid {
		case "google-antigravity":
			normalizedLegacyModel = "claude-opus-4-6-thinking"
		case "anthropic-api-key", "anthropic-setup-token":
			normalizedLegacyModel = "claude-sonnet-4-6"
		case "openai-api-key":
			normalizedLegacyModel = "gpt-5"
		default:
			normalizedLegacyModel = "gemini-3-pro-preview"
		}
	}
	return pid, buildProviderModelIDForSettings(pid, normalizedLegacyModel), normalizedLegacyModel
}

func resolveFallbackModelForSettings(providerID, fallbackModelID, legacyFallbackModel string) (string, string) {
	pid := normalizeGoogleProviderIDForSettings(providerID)
	if fp, fm := splitProviderModelIDForSettings(fallbackModelID); fm != "" {
		if fp != "" {
			pid = fp
		}
		model := normalizeModelAliasForProviderForSettings(pid, fm)
		return buildProviderModelIDForSettings(pid, model), model
	}
	legacy := normalizeModelAliasForProviderForSettings(pid, legacyFallbackModel)
	if legacy == "" {
		return "", ""
	}
	return buildProviderModelIDForSettings(pid, legacy), legacy
}

type fallbackModelCandidate struct {
	providerID string
	model      string
}

func normalizeFallbackChainForSettings(settings *Settings, providerID, primaryModel string) {
	if settings == nil {
		return
	}

	chain := resolveFallbackModelCandidatesForSettings(*settings, providerID, primaryModel)

	settings.FallbackModelID = ""
	settings.FallbackModel = ""
	settings.FallbackSecondModelID = ""
	settings.FallbackSecondModel = ""
	settings.FallbackThirdModelID = ""
	settings.FallbackThirdModel = ""

	if len(chain) > 0 {
		settings.FallbackModel = chain[0].model
		settings.FallbackModelID = buildProviderModelIDForSettings(chain[0].providerID, chain[0].model)
	}
	if len(chain) > 1 {
		settings.FallbackSecondModel = chain[1].model
		settings.FallbackSecondModelID = buildProviderModelIDForSettings(chain[1].providerID, chain[1].model)
	}
	if len(chain) > 2 {
		settings.FallbackThirdModel = chain[2].model
		settings.FallbackThirdModelID = buildProviderModelIDForSettings(chain[2].providerID, chain[2].model)
	}
}

func resolveFallbackModelCandidatesForSettings(settings Settings, providerID, primaryModel string) []fallbackModelCandidate {
	defaultProviderID := normalizeGoogleProviderIDForSettings(providerID)
	first := resolveFallbackModelCandidateForSettings(defaultProviderID, settings.FallbackModelID, settings.FallbackModel)
	second := resolveFallbackModelCandidateForSettings(defaultProviderID, settings.FallbackSecondModelID, settings.FallbackSecondModel)
	third := resolveFallbackModelCandidateForSettings(defaultProviderID, settings.FallbackThirdModelID, settings.FallbackThirdModel)
	return buildFallbackModelChainForSettings(defaultProviderID, primaryModel, first, second, third)
}

func resolveFallbackModelCandidateForSettings(defaultProviderID, fallbackModelID, legacyFallbackModel string) fallbackModelCandidate {
	pid := normalizeGoogleProviderIDForSettings(defaultProviderID)
	if fp, fm := splitProviderModelIDForSettings(fallbackModelID); fm != "" {
		if fp != "" {
			pid = fp
		}
		return fallbackModelCandidate{
			providerID: pid,
			model:      normalizeModelAliasForProviderForSettings(pid, fm),
		}
	}
	legacy := normalizeModelAliasForProviderForSettings(pid, legacyFallbackModel)
	if legacy == "" {
		return fallbackModelCandidate{}
	}
	return fallbackModelCandidate{
		providerID: pid,
		model:      legacy,
	}
}

func buildFallbackModelChainForSettings(primaryProviderID, primaryModel string, candidates ...fallbackModelCandidate) []fallbackModelCandidate {
	normalizedPrimary := normalizeModelAliasForProviderForSettings(primaryProviderID, primaryModel)
	seen := map[string]struct{}{}
	chain := make([]fallbackModelCandidate, 0, len(candidates))

	for _, candidate := range candidates {
		providerID := normalizeGoogleProviderIDForSettings(candidate.providerID)
		model := normalizeModelAliasForProviderForSettings(providerID, candidate.model)
		if model == "" {
			continue
		}
		if normalizedPrimary != "" && model == normalizedPrimary {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}

		if providerID == "" {
			providerID = "google-gemini-cli"
		}
		chain = append(chain, fallbackModelCandidate{
			providerID: providerID,
			model:      model,
		})
		if len(chain) >= 3 {
			break
		}
	}

	return chain
}

// ResolveFallbackModelChain returns normalized fallback models in priority order.
func ResolveFallbackModelChain(settings Settings, providerID, primaryModel string) []string {
	candidates := resolveFallbackModelCandidatesForSettings(settings, providerID, primaryModel)
	models := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.model == "" {
			continue
		}
		models = append(models, candidate.model)
	}
	return models
}

// ResolveFallbackModelIDChain returns normalized provider/model IDs in priority order.
func ResolveFallbackModelIDChain(settings Settings, providerID, primaryModel string) []string {
	candidates := resolveFallbackModelCandidatesForSettings(settings, providerID, primaryModel)
	modelIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.model == "" {
			continue
		}
		modelIDs = append(modelIDs, buildProviderModelIDForSettings(candidate.providerID, candidate.model))
	}
	return modelIDs
}

func normalizeGoogleProviderIDForSettings(providerID string) string {
	switch strings.TrimSpace(strings.ToLower(providerID)) {
	case "":
		return ""
	case "google-gemini-cli", "gemini-cli", "oauth", "cli":
		return "google-gemini-cli"
	case "google-antigravity":
		return "google-antigravity"
	case "aistudio-rpc", "aistudio", "ai-studio":
		return "aistudio-rpc"
	case "gemini-api-key":
		return "gemini-api-key"
	case "openai-api-key":
		return "openai-api-key"
	case "anthropic-api-key":
		return "anthropic-api-key"
	case "anthropic-setup-token":
		return "anthropic-setup-token"
	default:
		return strings.TrimSpace(strings.ToLower(providerID))
	}
}

func normalizeGoogleModelAliasForSettings(model string) string {
	// Legacy/global alias normalizer kept for backward compatibility.
	// New code should prefer normalizeModelAliasForProviderForSettings.
	normalized := strings.TrimSpace(model)
	if strings.HasPrefix(normalized, "models/") {
		normalized = strings.TrimPrefix(normalized, "models/")
	}
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 && idx+1 < len(normalized) {
		normalized = normalized[idx+1:]
	}
	switch normalized {
	case "gemini-3-pro":
		return "gemini-3-pro-preview"
	case "gemini-3-flash":
		return "gemini-3-flash-preview"
	case "claude-opus-4-6":
		return "claude-opus-4-6-thinking"
	case "claude-sonnet-4":
		return "claude-sonnet-4-thinking"
	default:
		return strings.TrimSpace(normalized)
	}
}

func normalizeModelAliasForProviderForSettings(providerID, model string) string {
	pid := normalizeGoogleProviderIDForSettings(providerID)
	normalized := strings.TrimSpace(model)
	if strings.HasPrefix(normalized, "models/") {
		normalized = strings.TrimPrefix(normalized, "models/")
	}
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 && idx+1 < len(normalized) {
		normalized = normalized[idx+1:]
	}
	switch normalized {
	case "gemini-3-pro":
		normalized = "gemini-3-pro-preview"
	case "gemini-3-flash":
		normalized = "gemini-3-flash-preview"
	}
	switch pid {
	case "google-antigravity", "":
		switch normalized {
		case "claude-opus-4-6":
			return "claude-opus-4-6-thinking"
		case "claude-sonnet-4":
			return "claude-sonnet-4-thinking"
		}
	}
	return strings.TrimSpace(normalized)
}

func splitProviderModelIDForSettings(modelID string) (string, string) {
	normalized := strings.TrimSpace(modelID)
	if normalized == "" {
		return "", ""
	}
	if idx := strings.Index(normalized, "/"); idx > 0 {
		pid := normalizeGoogleProviderIDForSettings(normalized[:idx])
		return pid, normalizeModelAliasForProviderForSettings(pid, normalized[idx+1:])
	}
	return "", normalizeGoogleModelAliasForSettings(normalized)
}

func buildProviderModelIDForSettings(providerID, model string) string {
	pid := normalizeGoogleProviderIDForSettings(providerID)
	mid := normalizeModelAliasForProviderForSettings(pid, model)
	if pid == "" {
		pid = "google-gemini-cli"
	}
	if mid == "" {
		switch pid {
		case "openai-api-key":
			mid = "gpt-5"
		case "anthropic-api-key", "anthropic-setup-token":
			mid = "claude-sonnet-4-6"
		default:
			mid = "gemini-3-pro-preview"
		}
	}
	return pid + "/" + mid
}

func syncLegacyProviderTypeForSettings(providerType, providerID, geminiAPIKey, anthropicAPIKey string) string {
	switch normalizeGoogleProviderIDForSettings(providerID) {
	case "gemini-api-key":
		return ProviderAPIKey
	case "openai-api-key":
		return ProviderAPIKey
	case "anthropic-api-key":
		return ProviderAPIKey
	case "anthropic-setup-token":
		return ProviderOAuth
	case "google-gemini-cli", "google-antigravity", "aistudio-rpc":
		return ProviderOAuth
	}
	normalized := normalizeProviderType(providerType)
	if normalized != "" {
		return normalized
	}
	if strings.TrimSpace(geminiAPIKey) != "" || strings.TrimSpace(anthropicAPIKey) != "" {
		return ProviderAPIKey
	}
	return ProviderOAuth
}

func normalizeEndpointPolicyForSettings(policy string) string {
	switch strings.TrimSpace(strings.ToLower(policy)) {
	case "primary_then_sandbox":
		return "primary_then_sandbox"
	default:
		return "primary_then_sandbox"
	}
}

func normalizeMCPSettings(mcp MCPSettings) MCPSettings {
	if mcp.DiscoveryTimeoutSeconds < MinMCPDiscoveryTimeoutSeconds {
		mcp.DiscoveryTimeoutSeconds = DefaultMCPDiscoveryTimeoutSeconds
	}
	if mcp.DiscoveryTimeoutSeconds > MaxMCPDiscoveryTimeoutSeconds {
		mcp.DiscoveryTimeoutSeconds = MaxMCPDiscoveryTimeoutSeconds
	}
	if mcp.ToolCallTimeoutSeconds < MinMCPToolCallTimeoutSeconds {
		mcp.ToolCallTimeoutSeconds = DefaultMCPToolCallTimeoutSeconds
	}
	if mcp.ToolCallTimeoutSeconds > MaxMCPToolCallTimeoutSeconds {
		mcp.ToolCallTimeoutSeconds = MaxMCPToolCallTimeoutSeconds
	}
	mcp.Servers = normalizeMCPServers(mcp.Servers)
	return mcp
}

func normalizeMCPServers(servers []MCPServerSetting) []MCPServerSetting {
	if len(servers) == 0 {
		return nil
	}
	out := make([]MCPServerSetting, 0, len(servers))
	for _, server := range servers {
		server.ID = strings.TrimSpace(server.ID)
		server.Name = strings.TrimSpace(server.Name)
		server.Transport = strings.TrimSpace(server.Transport)
		server.Command = strings.TrimSpace(server.Command)
		server.URL = strings.TrimSpace(server.URL)
		server.Args = normalizeStringSlice(server.Args)
		server.Env = normalizeStringMap(server.Env)
		server.Headers = normalizeStringMap(server.Headers)
		server.ToolAllowlist = normalizeStringSlice(server.ToolAllowlist)
		server.Instructions = strings.TrimSpace(server.Instructions)
		out = append(out, server)
	}
	return out
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
