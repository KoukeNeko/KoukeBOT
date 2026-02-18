import type { AIStudioSessionInput, AIStudioSessionStatus, AIStudioVerifyResponse } from '../types'
import { BASE, fetchJSON } from './client'

export function getAIStudioSessionStatus(): Promise<AIStudioSessionStatus> {
  return fetchJSON(`${BASE}/aistudio/session`)
}

export function putAIStudioSession(session: AIStudioSessionInput): Promise<{ status: string; configured: boolean }> {
  return fetchJSON(`${BASE}/aistudio/session`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(session),
  })
}

export function deleteAIStudioSession(): Promise<{ status: string }> {
  return fetchJSON(`${BASE}/aistudio/session`, { method: 'DELETE' })
}

export function verifyAIStudioSession(params?: {
  session?: AIStudioSessionInput
  model?: string
}): Promise<AIStudioVerifyResponse> {
  return fetchJSON(`${BASE}/aistudio/session/verify`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      session: params?.session,
      model: params?.model ?? '',
    }),
  })
}
