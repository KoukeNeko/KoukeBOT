export interface SettingsData {
  gemini_api_key: string
  gemini_model: string
  discord_token: string
  provider_type: string
  provider_id: string
  model_id: string
  fallback_enabled: boolean
  fallback_model_id: string
  endpoint_policy: 'primary_then_sandbox'
  deprecated_fields?: string[]
  brave_api_key: string
  web_search_enabled: boolean
  discord_use_reply: boolean
  perplexity_api_key: string
  search_provider: string
  web_fetch_enabled: boolean
  web_tool_mode: 'classic' | 'playwright_mcp'
  timezone: string
  time_format: string
  location: string
  memory_extraction_enabled: boolean
  memory_extraction_model: string
  memory_recent_rounds: number
  log_channel_id?: string
  discord_persona_id: string
  discord_observe_enabled: boolean
  discord_member_intent_enabled: boolean
  discord_presence_intent_enabled: boolean
  discord_observe_max_results: number
  discord_observe_message_scope: 'current_channel' | 'current_guild'
  tool_guard_enabled: boolean
  max_tool_rounds: number
  max_web_search_calls_per_turn: number
  force_final_answer_after_tool_limit: boolean
  transient_recovery_wait_enabled: boolean
  transient_recovery_wait_max_seconds: number
  discord_show_internal_tool_errors: boolean
  scheduled_monitor_max_per_user: number
  fallback_model_enabled: boolean
  fallback_model: string
  mcp: MCPSettingsData
  persona_lock_every_turns: number
  triplet_output_enabled: boolean
  analysis_visibility: 'visible' | 'hidden' | 'debug'
  negative_constraint_enabled: boolean
  negative_constraint_default: string
}

export interface MCPSettingsData {
  enabled: boolean
  verbose_logging: boolean
  discovery_timeout_seconds: number
  tool_call_timeout_seconds: number
  servers: MCPServerData[]
}

export interface MCPServerData {
  id: string
  name: string
  enabled: boolean
  transport: 'stdio' | 'http'
  command: string
  args: string[]
  env: Record<string, string>
  url: string
  headers: Record<string, string>
  tool_allowlist: string[]
}

export interface MCPToolEntry {
  name: string
  description: string
  input_schema: Record<string, unknown>
}

export interface MCPDiscoverResponse {
  tools: MCPToolEntry[]
}

export interface MCPStatusEntry {
  server_id: string
  enabled: boolean
  transport: string
  reachable: boolean
  last_error?: string
  discovered_tools: string[]
  registered_tools: string[]
  updated_at: string
}

export interface ModelEntry {
  id: string
  display_name: string
  provider_id?: string
  supports_text: boolean
  supports_multimodal: boolean
  supports_tools: boolean
  reasoning: boolean
}

export interface ProviderStatus {
  configured: boolean
  provider_name: string
  model: string
  active_provider_id?: string
  active_model_id?: string
  effective_model_id?: string
  effective_endpoint?: string
  last_fallback_reason?: string
}

export interface RuntimeMetrics {
  timestamp: string
  tool_guard_duplicate_blocked: number
  tool_guard_limit_blocked: number
  tool_guard_max_rounds: number
  tool_forced_final_success: number
  tool_forced_final_failed: number
  tool_loop_fallback: number
  pool_transient_wait_retry: number
  pool_transient_wait_retry_succeeded: number
  pool_transient_wait_retry_failed: number
  pool_all_accounts_exhausted: number
  pool_model_fallback: number
  pool_accounts_total: number
  pool_accounts_exhausted_current: number
}

export interface ScheduledTask {
  id: string
  type: 'reminder' | 'activity_monitor'
  status: 'active' | 'completed' | 'canceled' | 'failed'
  persona_id: string
  channel_id: string
  creator_user_id: string
  target_user_id: string
  reminder_text?: string
  start_at: string
  end_at?: string
  run_at?: string
  next_run_at?: string
  last_checked_at?: string
  created_at: string
  updated_at: string
}
