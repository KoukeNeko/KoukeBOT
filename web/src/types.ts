export interface PersonaSummary {
  id: string
  name: string
  description: string
  version: string
  author: string
  anchor_count: number
  anchor_tags: string[]
}

export interface PersonaDetail {
  meta: {
    id: string
    name: string
    description: string
    version: string
    author: string
  }
  system_template: string
  lore: string
  user_profile: string
  anchors: Anchor[]
  generation: GenerationConfig
  variables: Record<string, unknown>
}

export interface Anchor {
  id: string
  tags: string[]
  user_input: string
  bot_response: string
}

export interface GenerationConfig {
  temperature: number
  max_tokens: number
  top_p: number
  frequency_penalty: number
  presence_penalty: number
}

export interface ChatResponse {
  response: string
}

export interface SettingsData {
  gemini_api_key: string
  gemini_model: string
  discord_token: string
  provider_type: string
  brave_api_key: string
  web_search_enabled: boolean
  discord_use_reply: boolean
  perplexity_api_key: string    // OpenClaw 相容
  search_provider: string        // OpenClaw 相容
  web_fetch_enabled: boolean     // OpenClaw 相容

  // Time and location settings (OpenClaw 相容)
  timezone: string               // "Asia/Taipei", "America/New_York", "auto"
  time_format: string            // "auto", "12", "24"
  location: string               // Optional: explicit location "台北, 台灣"

  // Memory extraction settings
  memory_extraction_enabled: boolean
  memory_extraction_model: string  // e.g. "gemini-2.0-flash"
}

export interface ModelEntry {
  id: string
  display_name: string
}

export interface ProviderStatus {
  configured: boolean
  provider_name: string
  model: string
}

export interface OAuthDetectResult {
  found: boolean
  client_id?: string
  client_secret?: string
  error?: string
}

export interface OAuthStartResult {
  auth_url: string
}

export interface OAuthStatus {
  authenticated: boolean
  project_id?: string
  email?: string
  pool?: PoolEntry[]
  pool_size?: number
}

export interface PoolEntry {
  email: string
  status: 'available' | 'exhausted'
  exhausted_until?: string
  current?: boolean
}

export interface Message {
  role: string
  content: string
  display_text?: string
  raw_llm_input?: string
  raw_llm_output?: string
  timestamp?: string
  tool_calls?: ToolCall[]
  tool_results?: ToolResult[]
}

export interface ToolCall {
  id: string
  name: string
  input: Record<string, any>
}

export interface ToolResult {
  tool_call_id: string
  content: string
  is_error: boolean
}

export interface Conversation {
  id: string
  channel_id: string
  persona_id: string
  messages: Message[]
  created_at: string
  updated_at: string
}

export interface ConversationSummary {
  id: string
  channel_id: string
  persona_id: string
  persona_name: string
  message_count: number
  last_message?: string
  last_timestamp: string
  updated_at: string
}
