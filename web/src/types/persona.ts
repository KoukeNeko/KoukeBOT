export interface PersonaSummary {
  id: string
  name: string
  description: string
  version: string
  author: string
  anchor_count: number
  anchor_tags: string[]
}

export interface PersonaMeta {
  id: string
  name: string
  description: string
  version: string
  author: string
}

export interface GenerationConfig {
  temperature: number
  top_p: number
  frequency_penalty: number
  presence_penalty: number
}

export interface PersonaCoreConfig {
  meta: PersonaMeta
  system_template: string
  generation: GenerationConfig
  variables: Record<string, unknown>
}

export interface PersonaDetail {
  config_yaml: string
  lore: string
  user_profile: string
  anchors: Anchor[]
}

export interface Anchor {
  id: string
  tags: string[]
  user_input: string
  bot_response: string
}
