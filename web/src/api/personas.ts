import type { PersonaDetail, PersonaSummary } from '../types'
import { BASE, fetchJSON } from './client'

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
