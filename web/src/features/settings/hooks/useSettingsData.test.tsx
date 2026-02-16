import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../api'
import type { ProviderStatus, RuntimeMetrics, SettingsData } from '../../../types'
import { useSettingsData } from './useSettingsData'

vi.mock('../../../api', () => ({
  getProviderStatus: vi.fn(),
  getRuntimeMetrics: vi.fn(),
  getSettings: vi.fn(),
  listModels: vi.fn(),
  listPersonas: vi.fn(),
  listScheduledTasks: vi.fn(),
  updateSettings: vi.fn(),
}))

vi.mock('./useOAuthActions', () => ({
  useOAuthActions: () => ({
    refreshOAuthSnapshot: vi.fn().mockResolvedValue({ oauth: null, poolEntries: [] }),
    setOAuthStatus: vi.fn(),
    setPool: vi.fn(),
    setProviderTab: vi.fn(),
  }),
}))

const settingsFixture: SettingsData = {
  gemini_api_key: '',
  gemini_model: 'gemini-2.5-pro',
  discord_token: '',
  provider_type: 'api-key',
  brave_api_key: '',
  web_search_enabled: false,
  discord_use_reply: false,
  perplexity_api_key: '',
  search_provider: 'auto',
  web_fetch_enabled: false,
  web_tool_mode: 'classic',
  timezone: 'auto',
  time_format: 'auto',
  location: '',
  memory_extraction_enabled: false,
  memory_extraction_model: '',
  memory_recent_rounds: 50,
  log_channel_id: '',
  discord_persona_id: '',
  discord_observe_enabled: false,
  discord_member_intent_enabled: false,
  discord_presence_intent_enabled: false,
  discord_observe_max_results: 25,
  discord_observe_message_scope: 'current_guild',
  tool_guard_enabled: false,
  max_tool_rounds: 5,
  max_web_search_calls_per_turn: 2,
  force_final_answer_after_tool_limit: true,
  transient_recovery_wait_enabled: true,
  transient_recovery_wait_max_seconds: 5,
  discord_show_internal_tool_errors: true,
  scheduled_monitor_max_per_user: 3,
  mcp: {
    enabled: false,
    verbose_logging: false,
    discovery_timeout_seconds: 10,
    tool_call_timeout_seconds: 20,
    servers: [],
  },
  persona_lock_every_turns: 4,
  triplet_output_enabled: true,
  analysis_visibility: 'visible',
  negative_constraint_enabled: true,
  negative_constraint_default:
    'You are FORBIDDEN from switching into a generic assistant tone. Keep persona-specific stance, diction, and attitude.',
}

const providerStatusFixture: ProviderStatus = {
  configured: true,
  provider_name: 'gemini',
  model: 'gemini-2.5-pro',
}

const metricsFixture: RuntimeMetrics = {
  timestamp: '2026-02-15T00:00:00Z',
  tool_guard_duplicate_blocked: 0,
  tool_guard_limit_blocked: 0,
  tool_guard_max_rounds: 3,
  tool_forced_final_success: 0,
  tool_forced_final_failed: 0,
  tool_loop_fallback: 0,
  pool_transient_wait_retry: 0,
  pool_transient_wait_retry_succeeded: 0,
  pool_transient_wait_retry_failed: 0,
  pool_all_accounts_exhausted: 0,
  pool_accounts_total: 0,
  pool_accounts_exhausted_current: 0,
}

describe('useSettingsData', () => {
  beforeEach(() => {
    vi.clearAllMocks()

    vi.mocked(api.getSettings).mockResolvedValue(settingsFixture)
    vi.mocked(api.getProviderStatus).mockResolvedValue(providerStatusFixture)
    vi.mocked(api.getRuntimeMetrics).mockResolvedValue(metricsFixture)
    vi.mocked(api.listModels).mockResolvedValue([
      { id: 'gemini-2.5-pro', display_name: 'Gemini 2.5 Pro' },
    ])
    vi.mocked(api.listPersonas).mockResolvedValue([])
    vi.mocked(api.listScheduledTasks).mockResolvedValue([])
    vi.mocked(api.updateSettings).mockResolvedValue({ status: 'ok' })
  })

  it('shows error message when initial load fails', async () => {
    vi.mocked(api.getSettings).mockRejectedValueOnce(new Error('load-failed'))

    const { result } = renderHook(() => useSettingsData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(result.current.message).toEqual({
      type: 'err',
      text: 'load-failed',
    })
  })

  it('shows error message when save fails', async () => {
    vi.mocked(api.updateSettings).mockRejectedValueOnce(new Error('save-failed'))

    const { result } = renderHook(() => useSettingsData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    await act(async () => {
      await result.current.handleSave()
    })

    expect(result.current.saving).toBe(false)
    expect(result.current.message).toEqual({
      type: 'err',
      text: 'save-failed',
    })
  })
})
