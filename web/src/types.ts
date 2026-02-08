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
}
