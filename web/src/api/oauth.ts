import type {
  OAuthCompleteResult,
  OAuthDetectResult,
  OAuthStartResult,
  OAuthStatus,
  PoolEntry,
} from '../types'
import { BASE, fetchJSON } from './client'

export function detectGeminiCLI(): Promise<OAuthDetectResult> {
  return fetchJSON(`${BASE}/oauth/gemini/detect`, { method: 'POST' })
}

export function startGeminiOAuth(options?: {
  clientId?: string
  clientSecret?: string
  providerId?: string
  mode?: 'local' | 'remote'
  redirectUrl?: string
}): Promise<OAuthStartResult> {
  return fetchJSON(`${BASE}/oauth/gemini/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client_id: options?.clientId ?? '',
      client_secret: options?.clientSecret ?? '',
      provider_id: options?.providerId ?? '',
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

export function completeGeminiOAuth(flowId: string, callbackUrl: string, providerId?: string): Promise<OAuthCompleteResult> {
  return fetchJSON(`${BASE}/oauth/gemini/complete`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ flow_id: flowId, callback_url: callbackUrl, provider_id: providerId ?? '' }),
  })
}

export function getOAuthStatus(): Promise<OAuthStatus> {
  return fetchJSON(`${BASE}/oauth/gemini/status`)
}

export function importGeminiCLIToken(providerId?: string): Promise<{ email: string; project_id: string; tier_id?: string; endpoint_used?: string; provider_id?: string; status: string }> {
  const suffix = providerId ? `?provider_id=${encodeURIComponent(providerId)}` : ''
  return fetchJSON(`${BASE}/oauth/gemini/import-cli${suffix}`, { method: 'POST' })
}

export function importPastedToken(tokenJson: string, clientId?: string, clientSecret?: string, providerId?: string): Promise<{ email: string; project_id: string; tier_id?: string; endpoint_used?: string; provider_id?: string; status: string }> {
  return fetchJSON(`${BASE}/oauth/gemini/import-token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token_json: tokenJson, client_id: clientId ?? '', client_secret: clientSecret ?? '', provider_id: providerId ?? '' }),
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
