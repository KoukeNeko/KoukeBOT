package api

import "github.com/KoukeNeko/KoukeBOT/internal/infra/config"

type SettingsResponse struct {
	GeminiAPIKey                    string              `json:"gemini_api_key"`
	GeminiModel                     string              `json:"gemini_model"`
	DiscordToken                    string              `json:"discord_token"`
	ProviderType                    string              `json:"provider_type"`
	BraveAPIKey                     string              `json:"brave_api_key"`
	WebSearchEnabled                bool                `json:"web_search_enabled"`
	DiscordUseReply                 bool                `json:"discord_use_reply"`
	DiscordStripLeadingBotMention   bool                `json:"discord_strip_leading_bot_mention"`
	PerplexityAPIKey                string              `json:"perplexity_api_key"`
	SearchProvider                  string              `json:"search_provider"`
	WebFetchEnabled                 bool                `json:"web_fetch_enabled"`
	Timezone                        string              `json:"timezone"`
	TimeFormat                      string              `json:"time_format"`
	Location                        string              `json:"location"`
	ContextFlushEnabled             bool                `json:"context_flush_enabled"`
	ContextFlushThreshold           int                 `json:"context_flush_threshold"`
	ContextWindowSize               int                 `json:"context_window_size"`
	LogChannelID                    string              `json:"log_channel_id"`
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
	WebSearchEnabled                *bool                     `json:"web_search_enabled"`
	DiscordUseReply                 *bool                     `json:"discord_use_reply"`
	DiscordStripLeadingBotMention   *bool                     `json:"discord_strip_leading_bot_mention"`
	PerplexityAPIKey                string                    `json:"perplexity_api_key"`
	SearchProvider                  string                    `json:"search_provider"`
	WebFetchEnabled                 *bool                     `json:"web_fetch_enabled"`
	Timezone                        string                    `json:"timezone"`
	TimeFormat                      string                    `json:"time_format"`
	Location                        string                    `json:"location"`
	ContextFlushEnabled             *bool                     `json:"context_flush_enabled"`
	ContextFlushThreshold           *int                      `json:"context_flush_threshold"`
	ContextWindowSize               *int                      `json:"context_window_size"`
	LogChannelID                    string                    `json:"log_channel_id"`
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
	VerboseLogging          bool                `json:"verbose_logging"`
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
	VerboseLogging          bool                     `json:"verbose_logging"`
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

func maskKey(key string) string {
	if len(key) <= 4 {
		return key
	}
	return "***..." + key[len(key)-4:]
}

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
		VerboseLogging:          settings.VerboseLogging,
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
