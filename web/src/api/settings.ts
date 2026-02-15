import type { ModelEntry, ProviderStatus, RuntimeMetrics, SettingsData } from '../types'
import { BASE, fetchJSON } from './client'

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

export function listModels(): Promise<ModelEntry[]> {
  return fetchJSON(`${BASE}/models`)
}
