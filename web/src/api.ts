import type { PersonaSummary, PersonaDetail, ChatResponse, SettingsData, ProviderStatus, RuntimeMetrics, OAuthDetectResult, OAuthStartResult, OAuthCompleteResult, OAuthStatus, ModelEntry, ConversationSummary, Conversation, PoolEntry } from './types'

const BASE = '/api'

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `HTTP ${res.status}`)
  }
  return res.json()
}

export function listPersonas(): Promise<PersonaSummary[]> {
  return fetchJSON(`${BASE}/personas`)
}

export function getPersona(id: string): Promise<PersonaDetail> {
  return fetchJSON(`${BASE}/personas/${encodeURIComponent(id)}`)
}

export function updatePersona(id: string, data: PersonaDetail): Promise<{ status: string }> {
  return fetchJSON(`${BASE}/personas/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
}

export function reloadPersonas(): Promise<{ status: string; count: string }> {
  return fetchJSON(`${BASE}/personas/reload`, { method: 'POST' })
}

export function sendChat(
  personaId: string,
  message: string,
  channelId = 'web-test',
  conversationId?: string,
): Promise<ChatResponse> {
  return fetchJSON(`${BASE}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      channel_id: channelId,
      persona_id: personaId,
      conversation_id: conversationId ?? '',
      message,
    }),
  })
}

export function getSettings(): Promise<SettingsData> {
  return fetchJSON(`${BASE}/settings`)
}

export function updateSettings(data: SettingsData): Promise<{ status: string; warning?: string }> {
  return fetchJSON(`${BASE}/settings`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
}

export function getProviderStatus(): Promise<ProviderStatus> {
  return fetchJSON(`${BASE}/settings/status`)
}

export function getRuntimeMetrics(): Promise<RuntimeMetrics> {
  return fetchJSON(`${BASE}/metrics/runtime`)
}

export function detectGeminiCLI(): Promise<OAuthDetectResult> {
  return fetchJSON(`${BASE}/oauth/gemini/detect`, { method: 'POST' })
}

export function startGeminiOAuth(options?: {
  clientId?: string
  clientSecret?: string
  mode?: 'local' | 'remote'
  redirectUrl?: string
}): Promise<OAuthStartResult> {
  return fetchJSON(`${BASE}/oauth/gemini/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client_id: options?.clientId ?? '',
      client_secret: options?.clientSecret ?? '',
      mode: options?.mode ?? '',
      redirect_url: options?.redirectUrl ?? '',
    }),
  })
}

export function submitOAuthCallback(callbackUrl: string): Promise<OAuthCompleteResult> {
  return fetchJSON(`${BASE}/oauth/gemini/callback`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ callback_url: callbackUrl }),
  })
}

export function completeGeminiOAuth(flowId: string, callbackUrl: string): Promise<OAuthCompleteResult> {
  return fetchJSON(`${BASE}/oauth/gemini/complete`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ flow_id: flowId, callback_url: callbackUrl }),
  })
}

export function getOAuthStatus(): Promise<OAuthStatus> {
  return fetchJSON(`${BASE}/oauth/gemini/status`)
}

export function importGeminiCLIToken(): Promise<{ email: string; project_id: string; status: string }> {
  return fetchJSON(`${BASE}/oauth/gemini/import-cli`, { method: 'POST' })
}

export function importPastedToken(tokenJson: string, clientId?: string, clientSecret?: string): Promise<{ email: string; project_id: string; status: string }> {
  return fetchJSON(`${BASE}/oauth/gemini/import-token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token_json: tokenJson, client_id: clientId ?? '', client_secret: clientSecret ?? '' }),
  })
}

export function listModels(): Promise<ModelEntry[]> {
  return fetchJSON(`${BASE}/models`)
}

export function listConversations(): Promise<ConversationSummary[]> {
  return fetchJSON(`${BASE}/conversations`)
}

export function createConversationSession(personaId: string, channelId = 'web-test'): Promise<Conversation> {
  return fetchJSON(`${BASE}/conversations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      persona_id: personaId,
      channel_id: channelId,
    }),
  })
}

export function getConversation(channelId: string, personaId: string): Promise<Conversation> {
  return fetchJSON(`${BASE}/conversations/${encodeURIComponent(channelId)}/${encodeURIComponent(personaId)}`)
}

export function getConversationByID(conversationId: string): Promise<Conversation> {
  return fetchJSON(`${BASE}/conversations/by-id/${encodeURIComponent(conversationId)}`)
}

export function deleteConversation(channelId: string, personaId: string): Promise<{ status: string }> {
  return fetchJSON(`${BASE}/conversations/${encodeURIComponent(channelId)}/${encodeURIComponent(personaId)}`, {
    method: 'DELETE',
  })
}

export function deleteConversationByID(conversationId: string): Promise<{ status: string; new_conversation_id?: string }> {
  return fetchJSON(`${BASE}/conversations/by-id/${encodeURIComponent(conversationId)}`, {
    method: 'DELETE',
  })
}

// OAuth Pool
export function getOAuthPool(): Promise<PoolEntry[]> {
  return fetchJSON(`${BASE}/oauth/pool`)
}

export function removeOAuthPoolAccount(email: string): Promise<{ status: string; pool_size: number }> {
  return fetchJSON(`${BASE}/oauth/pool/${encodeURIComponent(email)}`, {
    method: 'DELETE',
  })
}
