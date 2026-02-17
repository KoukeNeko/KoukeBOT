import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { SettingsData } from '../../types'
import ToolsSection from './ToolsSection'

function buildForm(mode: SettingsData['web_tool_mode']): SettingsData {
  return {
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
  }
}

describe('ToolsSection', () => {
  it('shows classic controls when web_tool_mode=classic', () => {
    render(
      <MemoryRouter>
        <ToolsSection
          form={buildForm('classic')}
          setForm={vi.fn()}
          saving={false}
          onSave={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.getByText('Enable Web Search')).toBeTruthy()
    expect(screen.queryByText(/目前為 `playwright_mcp` 模式/)).toBeNull()
  })

  it('shows playwright notice when web_tool_mode=playwright_mcp', () => {
    const setForm = vi.fn()
    render(
      <MemoryRouter>
        <ToolsSection
          form={buildForm('playwright_mcp')}
          setForm={setForm}
          saving={false}
          onSave={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.queryByText('Enable Web Search')).toBeNull()
    expect(screen.getByText(/目前為 `playwright_mcp` 模式/)).toBeTruthy()
    expect(screen.getByRole('link', { name: '/mcp' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  })
})
