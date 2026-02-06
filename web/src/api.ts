import type { PersonaSummary, PersonaDetail, ChatResponse } from './types'

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
