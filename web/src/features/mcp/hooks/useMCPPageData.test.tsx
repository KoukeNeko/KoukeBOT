import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../api'
import type { SettingsData } from '../../../types'
import { useMCPPageData } from './useMCPPageData'

vi.mock('../../../api', () => ({
  discoverMCPServer: vi.fn(),
  getMCPStatus: vi.fn(),
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
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
    enabled: true,
    verbose_logging: false,
    discovery_timeout_seconds: 10,
    tool_call_timeout_seconds: 20,
    servers: [
      {
        id: 'emfont',
        name: 'emfont MCP',
        enabled: true,
        transport: 'http',
        command: '',
        args: [],
        env: {},
        url: 'https://font.emtech.cc/mcp',
        headers: {},
        tool_allowlist: ['list-fonts'],
      },
    ],
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
  request_queue_enabled: true,
  request_queue_capacity: 10,
  request_queue_timeout_seconds: 120,
}

describe('useMCPPageData', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getSettings).mockResolvedValue(settingsFixture)
    vi.mocked(api.getMCPStatus).mockResolvedValue([])
    vi.mocked(api.updateSettings).mockResolvedValue({ status: 'ok' })
    vi.mocked(api.discoverMCPServer).mockResolvedValue({
      tools: [{ name: 'list-fonts', description: 'list fonts', input_schema: {} }],
    })
  })

  it('loads MCP settings and status', async () => {
    const { result } = renderHook(() => useMCPPageData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(result.current.form.mcp.servers).toHaveLength(1)
    expect(result.current.mcpStatus).toEqual([])
  })

  it('saves MCP settings and refreshes data', async () => {
    const { result } = renderHook(() => useMCPPageData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    await act(async () => {
      await result.current.saveMCP()
    })

    expect(api.updateSettings).toHaveBeenCalledTimes(1)
    expect(result.current.message).toEqual({
      type: 'ok',
      text: 'MCP settings saved successfully.',
    })
  })

  it('tests enabled servers and reports summary', async () => {
    const { result } = renderHook(() => useMCPPageData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    await act(async () => {
      await result.current.testEnabledServers()
    })

    expect(api.discoverMCPServer).toHaveBeenCalledTimes(1)
    expect(result.current.message).toEqual({
      type: 'ok',
      text: 'MCP test 完成：1/1 servers passed.',
    })
  })

  it('auto-provisions managed playwright server in playwright mode', async () => {
    vi.mocked(api.getSettings).mockResolvedValueOnce({
      ...settingsFixture,
      web_tool_mode: 'playwright_mcp',
      mcp: {
        ...settingsFixture.mcp,
        servers: [],
      },
    })

    const { result } = renderHook(() => useMCPPageData())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(result.current.form.mcp.servers.map((server) => server.id)).toContain('playwright_mcp')
  })
})
