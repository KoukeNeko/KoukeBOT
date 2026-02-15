package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// --- Settings endpoints ---

type SettingsResponse struct {
	GeminiAPIKey                    string              `json:"gemini_api_key"`
	GeminiModel                     string              `json:"gemini_model"`
	DiscordToken                    string              `json:"discord_token"`
	ProviderType                    string              `json:"provider_type"`
	BraveAPIKey                     string              `json:"brave_api_key"`
	WebSearchEnabled                bool                `json:"web_search_enabled"`
	DiscordUseReply                 bool                `json:"discord_use_reply"`
	DiscordStripLeadingBotMention   bool                `json:"discord_strip_leading_bot_mention"`
	PerplexityAPIKey                string              `json:"perplexity_api_key"` // OpenClaw 相容
	SearchProvider                  string              `json:"search_provider"`    // OpenClaw 相容
	WebFetchEnabled                 bool                `json:"web_fetch_enabled"`  // OpenClaw 相容
	Timezone                        string              `json:"timezone"`           // OpenClaw 相容
	TimeFormat                      string              `json:"time_format"`        // OpenClaw 相容
	Location                        string              `json:"location"`           // OpenClaw 相容
	ContextFlushEnabled             bool                `json:"context_flush_enabled"`
	ContextFlushThreshold           int                 `json:"context_flush_threshold"`
	ContextWindowSize               int                 `json:"context_window_size"`
	LogChannelID                    string              `json:"log_channel_id"` // Discord channel ID for stdout logs
	DiscordPersonaID                string              `json:"discord_persona_id"`
	MemoryExtractionEnabled         bool                `json:"memory_extraction_enabled"`
	MemoryExtractionModel           string              `json:"memory_extraction_model"`
	MemoryRecentRounds              int                 `json:"memory_recent_rounds"`
	PersonaLockEveryTurns           int                 `json:"persona_lock_every_turns"`
	TripletOutputEnabled            bool                `json:"triplet_output_enabled"`
	AnalysisVisibility              string              `json:"analysis_visibility"`
	NegativeConstraintEnabled       bool                `json:"negative_constraint_enabled"`
	NegativeConstraintDefault       string              `json:"negative_constraint_default"`
	DiscordObserveEnabled           bool                `json:"discord_observe_enabled"`
	DiscordMemberIntentEnabled      bool                `json:"discord_member_intent_enabled"`
	DiscordPresenceIntentEnabled    bool                `json:"discord_presence_intent_enabled"`
	DiscordObserveMaxResults        int                 `json:"discord_observe_max_results"`
	DiscordObserveMessageScope      string              `json:"discord_observe_message_scope"`
	MaxToolRounds                   int                 `json:"max_tool_rounds"`
	MaxWebSearchCallsPerTurn        int                 `json:"max_web_search_calls_per_turn"`
	ForceFinalAnswerAfterToolLimit  bool                `json:"force_final_answer_after_tool_limit"`
	TransientRecoveryWaitEnabled    bool                `json:"transient_recovery_wait_enabled"`
	TransientRecoveryWaitMaxSeconds int                 `json:"transient_recovery_wait_max_seconds"`
	DiscordShowInternalToolErrors   bool                `json:"discord_show_internal_tool_errors"`
	ScheduledMonitorMaxPerUser      int                 `json:"scheduled_monitor_max_per_user"`
	MCP                             MCPSettingsResponse `json:"mcp"`
}

type SettingsUpdateRequest struct {
	GeminiAPIKey                    string                    `json:"gemini_api_key"`
	GeminiModel                     string                    `json:"gemini_model"`
	DiscordToken                    string                    `json:"discord_token"`
	ProviderType                    string                    `json:"provider_type"`
	BraveAPIKey                     string                    `json:"brave_api_key"`
	WebSearchEnabled                *bool                     `json:"web_search_enabled"` // Pointer to distinguish false from unset
	DiscordUseReply                 *bool                     `json:"discord_use_reply"`  // Pointer to distinguish false from unset
	DiscordStripLeadingBotMention   *bool                     `json:"discord_strip_leading_bot_mention"`
	PerplexityAPIKey                string                    `json:"perplexity_api_key"` // OpenClaw 相容
	SearchProvider                  string                    `json:"search_provider"`    // OpenClaw 相容
	WebFetchEnabled                 *bool                     `json:"web_fetch_enabled"`  // OpenClaw 相容
	Timezone                        string                    `json:"timezone"`           // OpenClaw 相容
	TimeFormat                      string                    `json:"time_format"`        // OpenClaw 相容
	Location                        string                    `json:"location"`           // OpenClaw 相容
	ContextFlushEnabled             *bool                     `json:"context_flush_enabled"`
	ContextFlushThreshold           *int                      `json:"context_flush_threshold"`
	ContextWindowSize               *int                      `json:"context_window_size"`
	LogChannelID                    string                    `json:"log_channel_id"` // Discord channel ID for stdout logs
	DiscordPersonaID                string                    `json:"discord_persona_id"`
	MemoryExtractionEnabled         *bool                     `json:"memory_extraction_enabled"`
	MemoryExtractionModel           string                    `json:"memory_extraction_model"`
	MemoryRecentRounds              *int                      `json:"memory_recent_rounds"`
	PersonaLockEveryTurns           *int                      `json:"persona_lock_every_turns"`
	TripletOutputEnabled            *bool                     `json:"triplet_output_enabled"`
	AnalysisVisibility              string                    `json:"analysis_visibility"`
	NegativeConstraintEnabled       *bool                     `json:"negative_constraint_enabled"`
	NegativeConstraintDefault       string                    `json:"negative_constraint_default"`
	DiscordObserveEnabled           *bool                     `json:"discord_observe_enabled"`
	DiscordMemberIntentEnabled      *bool                     `json:"discord_member_intent_enabled"`
	DiscordPresenceIntentEnabled    *bool                     `json:"discord_presence_intent_enabled"`
	DiscordObserveMaxResults        *int                      `json:"discord_observe_max_results"`
	DiscordObserveMessageScope      string                    `json:"discord_observe_message_scope"`
	MaxToolRounds                   *int                      `json:"max_tool_rounds"`
	MaxWebSearchCallsPerTurn        *int                      `json:"max_web_search_calls_per_turn"`
	ForceFinalAnswerAfterToolLimit  *bool                     `json:"force_final_answer_after_tool_limit"`
	TransientRecoveryWaitEnabled    *bool                     `json:"transient_recovery_wait_enabled"`
	TransientRecoveryWaitMaxSeconds *int                      `json:"transient_recovery_wait_max_seconds"`
	DiscordShowInternalToolErrors   *bool                     `json:"discord_show_internal_tool_errors"`
	ScheduledMonitorMaxPerUser      *int                      `json:"scheduled_monitor_max_per_user"`
	MCP                             *MCPSettingsUpdateRequest `json:"mcp"`
}

type MCPSettingsResponse struct {
	Enabled                 bool                `json:"enabled"`
	DiscoveryTimeoutSeconds int                 `json:"discovery_timeout_seconds"`
	ToolCallTimeoutSeconds  int                 `json:"tool_call_timeout_seconds"`
	Servers                 []MCPServerResponse `json:"servers"`
}

type MCPServerResponse struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Enabled       bool              `json:"enabled"`
	Transport     string            `json:"transport"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	ToolAllowlist []string          `json:"tool_allowlist"`
}

type MCPSettingsUpdateRequest struct {
	Enabled                 bool                     `json:"enabled"`
	DiscoveryTimeoutSeconds int                      `json:"discovery_timeout_seconds"`
	ToolCallTimeoutSeconds  int                      `json:"tool_call_timeout_seconds"`
	Servers                 []MCPServerUpdateRequest `json:"servers"`
}

type MCPServerUpdateRequest struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Enabled       bool              `json:"enabled"`
	Transport     string            `json:"transport"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	ToolAllowlist []string          `json:"tool_allowlist"`
}

type ProviderStatusResponse struct {
	Configured   bool   `json:"configured"`
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
}

func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	// 確保 SearchProvider 有預設值
	searchProvider := settings.SearchProvider
	if searchProvider == "" {
		searchProvider = "auto"
	}
	discordPersonaID := settings.DiscordPersonaID
	if discordPersonaID == "" && h.discordPersona != nil {
		discordPersonaID = h.discordPersona.PersonaID()
	}

	writeJSON(w, http.StatusOK, SettingsResponse{
		GeminiAPIKey:                    maskKey(settings.GeminiAPIKey),
		GeminiModel:                     settings.GeminiModel,
		DiscordToken:                    maskKey(settings.DiscordToken),
		ProviderType:                    settings.ProviderType,
		BraveAPIKey:                     maskKey(settings.BraveAPIKey),
		WebSearchEnabled:                settings.WebSearchEnabled,
		DiscordUseReply:                 settings.DiscordUseReply,
		DiscordStripLeadingBotMention:   settings.DiscordStripLeadingBotMention,
		PerplexityAPIKey:                maskKey(settings.PerplexityAPIKey),
		SearchProvider:                  searchProvider,
		WebFetchEnabled:                 settings.WebFetchEnabled,
		Timezone:                        settings.Timezone,
		TimeFormat:                      settings.TimeFormat,
		Location:                        settings.Location,
		ContextFlushEnabled:             settings.ContextFlushEnabled,
		ContextFlushThreshold:           settings.ContextFlushThreshold,
		ContextWindowSize:               settings.ContextWindowSize,
		LogChannelID:                    settings.LogChannelID,
		DiscordPersonaID:                discordPersonaID,
		MemoryExtractionEnabled:         settings.MemoryExtractionEnabled,
		MemoryExtractionModel:           settings.MemoryExtractionModel,
		MemoryRecentRounds:              settings.MemoryRecentRounds,
		PersonaLockEveryTurns:           settings.PersonaLockEveryTurns,
		TripletOutputEnabled:            settings.TripletOutputEnabled,
		AnalysisVisibility:              settings.AnalysisVisibility,
		NegativeConstraintEnabled:       settings.NegativeConstraintEnabled,
		NegativeConstraintDefault:       settings.NegativeConstraintDefault,
		DiscordObserveEnabled:           settings.DiscordObserveEnabled,
		DiscordMemberIntentEnabled:      settings.DiscordMemberIntentEnabled,
		DiscordPresenceIntentEnabled:    settings.DiscordPresenceIntentEnabled,
		DiscordObserveMaxResults:        settings.DiscordObserveMaxResults,
		DiscordObserveMessageScope:      settings.DiscordObserveMessageScope,
		MaxToolRounds:                   settings.MaxToolRounds,
		MaxWebSearchCallsPerTurn:        settings.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit:  settings.ForceFinalAnswerAfterToolLimit,
		TransientRecoveryWaitEnabled:    settings.TransientRecoveryWaitEnabled,
		TransientRecoveryWaitMaxSeconds: settings.TransientRecoveryWaitMaxSeconds,
		DiscordShowInternalToolErrors:   settings.DiscordShowInternalToolErrors,
		ScheduledMonitorMaxPerUser:      settings.ScheduledMonitorMaxPerUser,
		MCP:                             toMCPSettingsResponse(settings.MCP),
	})
}

func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	req, err := decodeSettingsUpdateRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	if err := h.applySettingsPatch(&existing, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateUpdatedSettings(existing); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	response, err := h.saveAndHotSwapSettings(existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// activateProvider creates and hot-swaps the LLM provider based on settings.
func (h *Handlers) activateProvider(s config.Settings) error {
	model := s.GeminiModel
	if model == "" {
		model = "gemini-2.5-pro"
	}

	logAPI.Logf("activate-provider: type=%s model=%s", s.ProviderType, model)

	switch s.ProviderType {
	case config.ProviderOAuth:
		logAPI.Logf("activate-provider: switching to OAuth")
		// Load all pool tokens and build a ProviderPool (not just a single token)
		poolPath := h.oauthFlow.PoolPath()
		tokens, err := llm.LoadOAuthPool(poolPath)
		if err != nil || len(tokens) == 0 {
			// Fallback to single token
			oauthData, err := h.oauthFlow.GetStatus()
			if err != nil || oauthData == nil {
				return fmt.Errorf("no OAuth credentials available")
			}
			tokens = []*llm.OAuthTokenData{oauthData}
		}
		pool := app.NewProviderPool()
		pool.SetPoolStorePath(poolPath)
		if s.AutoRetryCount > 0 {
			pool.SetAutoRetryCount(s.AutoRetryCount)
		}
		pool.SetTransientRecoveryWait(
			s.TransientRecoveryWaitEnabled,
			time.Duration(s.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
		for _, tokenData := range tokens {
			if err := llm.RefreshIfNeeded(h.ctx, tokenData, ""); err != nil {
				logAPI.Warnf("activate-provider: token refresh failed for %s: %v", tokenData.Email, err)
				continue
			}
			if poolPath != "" && tokenData.Email != "" {
				if err := llm.UpdateOAuthPoolToken(poolPath, tokenData.Email, tokenData); err != nil {
					logAPI.Warnf("activate-provider: persist refreshed token failed for %s: %v", tokenData.Email, err)
				}
			}
			p, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
			if err != nil {
				logAPI.Warnf("activate-provider: provider init failed for %s: %v", tokenData.Email, err)
				continue
			}
			pool.AddOrReplaceWithUsage(p, tokenData.Email, tokenData.UsageStats)
		}
		if pool.Size() == 0 {
			return fmt.Errorf("no OAuth accounts available")
		}
		h.providerManager.Update(pool)
		logAPI.Logf("provider hot-swapped: OAuth pool (%d accounts, model: %s)", pool.Size(), model)

	case config.ProviderAPIKey:
		if s.GeminiAPIKey == "" {
			return fmt.Errorf("no API key configured")
		}
		provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
		if err != nil {
			return fmt.Errorf("invalid API key: %w", err)
		}
		h.providerManager.Update(provider)
		logAPI.Logf("provider hot-swapped: gemini-api (model: %s)", model)

	default:
		// Auto mode: try API key first, then OAuth.
		if s.GeminiAPIKey != "" {
			provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
			if err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}
			h.providerManager.Update(provider)
			logAPI.Logf("provider hot-swapped: gemini-api (model: %s)", model)
		}
	}
	return nil
}

func (h *Handlers) GetProviderStatus(w http.ResponseWriter, r *http.Request) {
	info := h.providerManager.ModelInfo()
	writeJSON(w, http.StatusOK, ProviderStatusResponse{
		Configured:   h.providerManager.IsConfigured(),
		ProviderName: info.ProviderName,
		Model:        info.ProviderName, // ModelInfo doesn't expose model name separately
	})
}

// --- Models endpoint ---

func (h *Handlers) ListModels(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	var oauthToken *llm.OAuthTokenData
	if data, err := h.oauthFlow.GetStatus(); err == nil {
		oauthToken = data
	}

	models, err := llm.ListGeminiModels(r.Context(), settings.GeminiAPIKey, oauthToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, models)
}

// maskKey shows only the last 4 characters of a secret, e.g. "***...abcd".
func maskKey(key string) string {
	if len(key) <= 4 {
		return key
	}
	return "***..." + key[len(key)-4:]
}

// isMasked checks if a value looks like a masked placeholder from GetSettings.
func isMasked(val string) bool {
	return len(val) > 3 && val[:3] == "***"
}

func toMCPSettingsResponse(settings config.MCPSettings) MCPSettingsResponse {
	servers := make([]MCPServerResponse, 0, len(settings.Servers))
	for _, server := range settings.Servers {
		servers = append(servers, MCPServerResponse{
			ID:            server.ID,
			Name:          server.Name,
			Enabled:       server.Enabled,
			Transport:     server.Transport,
			Command:       server.Command,
			Args:          append([]string(nil), server.Args...),
			Env:           maskSecretMap(server.Env),
			URL:           server.URL,
			Headers:       maskSecretMap(server.Headers),
			ToolAllowlist: append([]string(nil), server.ToolAllowlist...),
		})
	}
	return MCPSettingsResponse{
		Enabled:                 settings.Enabled,
		DiscoveryTimeoutSeconds: settings.DiscoveryTimeoutSeconds,
		ToolCallTimeoutSeconds:  settings.ToolCallTimeoutSeconds,
		Servers:                 servers,
	}
}

func maskSecretMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = maskKey(value)
	}
	return out
}

func validatePromptGuardSettings(lockEvery int, analysisVisibility string, negativeConstraintEnabled bool, negativeConstraintDefault string) error {
	if lockEvery < 1 || lockEvery > 16 {
		return fmt.Errorf("persona_lock_every_turns must be between 1 and 16")
	}
	switch analysisVisibility {
	case app.AnalysisVisibilityVisible, app.AnalysisVisibilityHidden, app.AnalysisVisibilityDebug:
		// valid
	default:
		return fmt.Errorf("analysis_visibility must be one of: visible, hidden, debug")
	}
	if negativeConstraintEnabled && strings.TrimSpace(negativeConstraintDefault) == "" {
		return fmt.Errorf("negative_constraint_default cannot be empty when negative constraints are enabled")
	}
	return nil
}

func validateDiscordObserveSettings(maxResults int, messageScope string) error {
	if maxResults < 1 || maxResults > 100 {
		return fmt.Errorf("discord_observe_max_results must be between 1 and 100")
	}
	switch messageScope {
	case config.DiscordObserveScopeCurrentChannel, config.DiscordObserveScopeCurrentGuild:
		return nil
	default:
		return fmt.Errorf("discord_observe_message_scope must be one of: %s, %s",
			config.DiscordObserveScopeCurrentChannel, config.DiscordObserveScopeCurrentGuild)
	}
}

func validateToolLoopSettings(maxToolRounds, maxWebSearchCallsPerTurn, transientRecoveryWaitMaxSeconds int) error {
	if maxToolRounds < 1 || maxToolRounds > 6 {
		return fmt.Errorf("max_tool_rounds must be between 1 and 6")
	}
	if maxWebSearchCallsPerTurn < 1 || maxWebSearchCallsPerTurn > 4 {
		return fmt.Errorf("max_web_search_calls_per_turn must be between 1 and 4")
	}
	if transientRecoveryWaitMaxSeconds < 1 || transientRecoveryWaitMaxSeconds > 15 {
		return fmt.Errorf("transient_recovery_wait_max_seconds must be between 1 and 15")
	}
	return nil
}

func validateSchedulerSettings(scheduledMonitorMaxPerUser int) error {
	if scheduledMonitorMaxPerUser < 1 || scheduledMonitorMaxPerUser > config.MaxScheduledMonitorMaxPerUser {
		return fmt.Errorf(
			"scheduled_monitor_max_per_user must be between 1 and %d",
			config.MaxScheduledMonitorMaxPerUser,
		)
	}
	return nil
}

func validateMemoryRetentionSettings(memoryRecentRounds int) error {
	if memoryRecentRounds < config.MinMemoryRecentRounds || memoryRecentRounds > config.MaxMemoryRecentRounds {
		return fmt.Errorf(
			"memory_recent_rounds must be between %d and %d",
			config.MinMemoryRecentRounds,
			config.MaxMemoryRecentRounds,
		)
	}
	return nil
}
