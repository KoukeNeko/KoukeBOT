import type { PersonaSummary, PersonaDetail, ChatResponse, SettingsData, ProviderStatus, OAuthDetectResult, OAuthStartResult, OAuthStatus, ModelEntry, ConversationSummary, Conversation, PoolEntry } from './types'

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
): Promise<ChatResponse> {
  return fetchJSON(`${BASE}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      channel_id: channelId,
      persona_id: personaId,
      message,
    }),
  })
}

export function getSettings(): Promise<SettingsData> {
  return fetchJSON(`${BASE}/settings`)
}

export function updateSettings(data: SettingsData): Promise<{ status: string }> {
  return fetchJSON(`${BASE}/settings`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
}

export function getProviderStatus(): Promise<ProviderStatus> {
  return fetchJSON(`${BASE}/settings/status`)
}

export function detectGeminiCLI(): Promise<OAuthDetectResult> {
  return fetchJSON(`${BASE}/oauth/gemini/detect`, { method: 'POST' })
}

export function startGeminiOAuth(clientId?: string, clientSecret?: string): Promise<OAuthStartResult> {
  return fetchJSON(`${BASE}/oauth/gemini/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client_id: clientId ?? '', client_secret: clientSecret ?? '' }),
  })
}

export function submitOAuthCallback(callbackUrl: string): Promise<{ email: string; status: string }> {
  return fetchJSON(`${BASE}/oauth/gemini/callback`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ callback_url: callbackUrl }),
  })
}

export function getOAuthStatus(): Promise<OAuthStatus> {
  return fetchJSON(`${BASE}/oauth/gemini/status`)
}

export function listModels(): Promise<ModelEntry[]> {
  return fetchJSON(`${BASE}/models`)
}

export function listConversations(): Promise<ConversationSummary[]> {
  return fetchJSON(`${BASE}/conversations`)
}

export function getConversation(channelId: string, personaId: string): Promise<Conversation> {
  return fetchJSON(`${BASE}/conversations/${encodeURIComponent(channelId)}/${encodeURIComponent(personaId)}`)
}

export function deleteConversation(channelId: string, personaId: string): Promise<{ status: string }> {
  return fetchJSON(`${BASE}/conversations/${encodeURIComponent(channelId)}/${encodeURIComponent(personaId)}`, {
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
