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

vi.mock('./useAIStudioActions', () => ({
  useAIStudioActions: () => ({
    sessionStatus: null,
    selectedFileName: '',
    parsedImport: null,
    statusMessage: null,
    uploading: false,
    verifyingCurrent: false,
    deleting: false,
    setStatusMessage: vi.fn(),
    refreshSessionStatus: vi.fn().mockResolvedValue({ configured: false }),
    parseUploadFile: vi.fn(),
    uploadVerifyApply: vi.fn(),
    verifyCurrentSession: vi.fn(),
    deleteSession: vi.fn(),
  }),
}))

const settingsFixture: SettingsData = {
  gemini_api_key: '',
  gemini_model: 'gemini-2.5-pro',
  discord_token: '',
  provider_type: 'api-key',
  provider_id: 'gemini-api-key',
  model_id: 'gemini-api-key/gemini-2.5-pro',
  fallback_enabled: false,
  fallback_model_id: '',
  fallback_second_model_id: '',
  fallback_third_model_id: '',
  endpoint_policy: 'primary_then_sandbox',
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
  fallback_model_enabled: false,
  fallback_model: '',
  fallback_second_model: '',
  fallback_third_model: '',
  request_queue_enabled: true,
  request_queue_capacity: 10,
  request_queue_timeout_seconds: 120,
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
  pool_model_fallback: 0,
  pool_oauth_exhaustion_fallback_try: 0,
  pool_oauth_exhaustion_fallback_ok: 0,
  pool_oauth_exhaustion_fallback_fail: 0,
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
      {
        id: 'gemini-api-key/gemini-2.5-pro',
        display_name: 'Gemini 2.5 Pro',
        provider_id: 'gemini-api-key',
        supports_text: true,
        supports_multimodal: true,
        supports_tools: true,
        reasoning: true,
      },
    ])
    vi.mocked(api.listPersonas).mockResolvedValue([])
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

  it('keeps aistudio-rpc provider when saving oauth settings', async () => {
    vi.mocked(api.getSettings).mockResolvedValueOnce({
      ...settingsFixture,
      provider_type: 'oauth',
      provider_id: 'aistudio-rpc',
      model_id: 'aistudio-rpc/gemini-3-pro-preview',
      gemini_model: 'gemini-3-pro-preview',
    })
    vi.mocked(api.listModels).mockResolvedValue([
      {
        id: 'aistudio-rpc/gemini-3-pro-preview',
        display_name: 'AI Studio RPC / Gemini 3 Pro (Preview)',
        provider_id: 'aistudio-rpc',
        supports_text: true,
        supports_multimodal: false,
        supports_tools: false,
        reasoning: true,
      },
    ])

    const { result } = renderHook(() => useSettingsData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    await act(async () => {
      await result.current.handleSave('oauth')
    })

    expect(api.updateSettings).toHaveBeenCalled()
    const lastCall = vi.mocked(api.updateSettings).mock.calls.at(-1)
    expect(lastCall?.[0]).toMatchObject({
      provider_type: 'oauth',
      provider_id: 'aistudio-rpc',
    })
  })

  it('includes second/third fallback model fields in save payload', async () => {
    const { result } = renderHook(() => useSettingsData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    act(() => {
      result.current.setForm({
        ...result.current.form,
        fallback_model_enabled: true,
        fallback_model_id: 'gemini-api-key/gemini-2.5-pro',
        fallback_model: 'gemini-2.5-pro',
        fallback_second_model_id: 'gemini-api-key/gemini-2.0-flash',
        fallback_second_model: 'gemini-2.0-flash',
        fallback_third_model_id: 'gemini-api-key/gemini-2.5-flash',
        fallback_third_model: 'gemini-2.5-flash',
      })
    })

    await act(async () => {
      await result.current.handleSave()
    })

    const lastCall = vi.mocked(api.updateSettings).mock.calls.at(-1)
    expect(lastCall?.[0]).toMatchObject({
      fallback_model_id: 'gemini-api-key/gemini-2.5-pro',
      fallback_model: 'gemini-2.5-pro',
      fallback_second_model_id: 'gemini-api-key/gemini-2.0-flash',
      fallback_second_model: 'gemini-2.0-flash',
      fallback_third_model_id: 'gemini-api-key/gemini-2.5-flash',
      fallback_third_model: 'gemini-2.5-flash',
    })
  })
})
