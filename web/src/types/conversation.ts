export interface ChatResponse {
  response: string
}

export interface Message {
  role: string
  content: string
  display_text?: string
  raw_llm_input?: string
  raw_llm_input_human?: string
  raw_llm_output?: string
  timestamp?: string
  tool_calls?: ToolCall[]
  tool_results?: ToolResult[]
}

export interface ToolCall {
  id: string
  name: string
  input: Record<string, unknown>
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
  context_digests?: ConversationDigest[]
  history_cutoff_at?: string
  created_at: string
  updated_at: string
}

export interface ConversationDigest {
  id: string
  summary: string
  facts?: string[]
  preferences?: string[]
  open_items?: string[]
  decisions?: string[]
  keywords?: string[]
  attitude?: string
  emotional_tone?: string
  style_markers?: string[]
  signature_terms?: string[]
  style_quotes?: string[]
  created_at: string
}

export interface ConversationSummary {
  id: string
  channel_id: string
  persona_id: string
  persona_name: string
  is_active?: boolean
  message_count: number
  last_message?: string
  last_timestamp: string
  updated_at: string
}
