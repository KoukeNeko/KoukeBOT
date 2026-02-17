export interface OAuthDetectResult {
  found: boolean
  client_id?: string
  client_secret?: string
  error?: string
}

export interface OAuthStartResult {
  flow_id: string
  auth_url: string
  expires_at: string
  mode: 'local' | 'remote'
  provider_id?: string
}

export interface OAuthCompleteResult {
  email: string
  project_id: string
  tier_id?: string
  endpoint_used?: string
  provider_id?: string
  status: string
}

export interface OAuthStatus {
  authenticated: boolean
  project_id?: string
  email?: string
  provider_id?: string
  tier_id?: string
  endpoint_used?: string
  pool?: PoolEntry[]
  pool_size?: number
}

export interface PoolEntry {
  email: string
  status: 'available' | 'exhausted'
  exhausted_until?: string
  current?: boolean

  request_count: number
  total_requests: number
  last_reset_time: string
  attempts_24h?: number
  successes_24h?: number
  failures_24h?: number
  attempts_total?: number
  successes_total?: number
  failures_total?: number
  last_attempt_at?: string

  consecutive_failures?: number
  last_error?: string
  last_success_at?: string
}
