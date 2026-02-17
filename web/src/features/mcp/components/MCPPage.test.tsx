import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { SettingsData } from '../../../types'
import MCPPage from './MCPPage'

function buildForm(mode: SettingsData['web_tool_mode'], serverID: string): SettingsData {
  return {
    gemini_api_key: '',
    gemini_model: '',
    discord_token: '',
    provider_type: '',
    brave_api_key: '',
    web_search_enabled: false,
    discord_use_reply: false,
    perplexity_api_key: '',
    search_provider: 'auto',
    web_fetch_enabled: false,
    web_tool_mode: mode,
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
          id: serverID,
          name: serverID,
          enabled: true,
          transport: 'stdio',
          command: serverID === 'playwright_mcp' ? 'playwright-mcp' : '',
          args: serverID === 'playwright_mcp' ? ['--headless', '--no-sandbox', '--isolated'] : [],
          env: {},
          url: '',
          headers: {},
          tool_allowlist: [],
        },
      ],
    },
    persona_lock_every_turns: 4,
    triplet_output_enabled: true,
    analysis_visibility: 'visible',
    negative_constraint_enabled: true,
    negative_constraint_default: 'x',
    fallback_model_enabled: false,
    fallback_model: '',
  }
}

describe('MCPPage', () => {
  it('renders page and triggers test/discover actions', async () => {
    const testEnabledServers = vi.fn().mockResolvedValue(undefined)
    const refreshStatus = vi.fn().mockResolvedValue(undefined)
    const discoverServer = vi.fn().mockResolvedValue([])
    const saveMCP = vi.fn().mockResolvedValue(undefined)

    render(
      <MCPPage
        vm={{
          form: buildForm('classic', 'emfont'),
          setForm: vi.fn(),
          mcpStatus: [],
          discovered: {},
          loading: false,
          saving: false,
          testing: false,
          message: null,
          saveMCP,
          discoverServer,
          testEnabledServers,
          refreshStatus,
        }}
      />,
    )

    expect(screen.getByRole('heading', { name: 'MCP', level: 2 })).toBeTruthy()

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Test Enabled Servers' }))
    })
    expect(testEnabledServers).toHaveBeenCalledTimes(1)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Refresh Status' }))
    })
    expect(refreshStatus).toHaveBeenCalledTimes(1)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Discover tools' }))
    })
    expect(discoverServer).toHaveBeenCalledTimes(1)
  })

  it('keeps managed playwright server readonly in playwright mode', () => {
    render(
      <MCPPage
        vm={{
          form: buildForm('playwright_mcp', 'playwright_mcp'),
          setForm: vi.fn(),
          mcpStatus: [],
          discovered: {},
          loading: false,
          saving: false,
          testing: false,
          message: null,
          saveMCP: vi.fn(),
          discoverServer: vi.fn().mockResolvedValue([]),
          testEnabledServers: vi.fn().mockResolvedValue(undefined),
          refreshStatus: vi.fn().mockResolvedValue(undefined),
        }}
      />,
    )

    expect(screen.getByText('managed')).toBeTruthy()
    expect(screen.queryByRole('button', { name: '刪除' })).toBeNull()
    const commandInput = screen.getByDisplayValue('playwright-mcp') as HTMLInputElement
    expect(commandInput.disabled).toBe(true)
  })
})
