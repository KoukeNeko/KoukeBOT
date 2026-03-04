import { render, screen } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { SettingsData } from '../../types'
import ProviderSection from './ProviderSection'

function buildForm(): SettingsData {
  return {
    gemini_api_key: '',
    anthropic_api_key: '',
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
}

function buildProps(overrides: Partial<ComponentProps<typeof ProviderSection>> = {}): ComponentProps<typeof ProviderSection> {
  const noopAsyncVoid = async () => undefined
  const noopAsyncBool = async () => true
  return {
    form: buildForm(),
    setForm: vi.fn(),
    modelOptions: [{ value: 'gemini-api-key/gemini-2.5-pro', label: 'Gemini 2.5 Pro' }],
    providerTab: 'api-key',
    setProviderTab: vi.fn(),
    oauthStatus: null,
    pool: [],
    removingEmail: null,
    onRemovePoolAccount: vi.fn(async () => undefined),
    verifyingPool: false,
    onVerifyPool: vi.fn(async () => undefined),
    saving: false,
    onSave: vi.fn(),
    addAccountOpen: false,
    onOpenAddAccount: vi.fn(),
    onCloseAddAccount: vi.fn(),
    oauthStarting: false,
    oauthCompleting: false,
    oauthMode: 'local',
    setOAuthMode: vi.fn(),
    oauthFlow: null,
    oauthCallbackURL: '',
    setOAuthCallbackURL: vi.fn(),
    onStartOAuth: vi.fn(),
    onCompleteOAuth: vi.fn(),
    message: null,
    pastedTokenJSON: '',
    setPastedTokenJSON: vi.fn(),
    importingPasted: false,
    onImportPastedToken: vi.fn(),
    importingCLI: false,
    onImportCLIToken: vi.fn(),
    anthropicSetupToken: '',
    setAnthropicSetupToken: vi.fn(),
    importingAnthropicToken: false,
    importingAnthropicEnv: false,
    onImportAnthropicToken: vi.fn(),
    onImportAnthropicEnv: vi.fn(),
    aistudioStatus: { configured: false },
    aistudioFileName: '',
    aistudioPastedInput: '',
    aistudioImport: null,
    aistudioMessage: null,
    aistudioUploading: false,
    aistudioVerifyingCurrent: false,
    aistudioDeleting: false,
    setAIStudioPastedInput: vi.fn(),
    onAIStudioFileSelected: vi.fn(noopAsyncVoid),
    onAIStudioParsePastedInput: vi.fn(() => true),
    onAIStudioUploadVerifyApply: vi.fn(noopAsyncBool),
    onAIStudioVerifyCurrent: vi.fn(noopAsyncBool),
    onAIStudioDelete: vi.fn(noopAsyncBool),
    ...overrides,
  }
}

describe('ProviderSection', () => {
  it('renders AI Studio upload controls', () => {
    render(<ProviderSection {...buildProps()} />)

    expect(screen.getByText('AI Studio Fallback')).toBeTruthy()
    expect(screen.getByText('Upload & Verify & Apply')).toBeTruthy()
    expect(screen.getByText('Parse Pasted Text')).toBeTruthy()
    expect(screen.getByText('Verify Current Session')).toBeTruthy()
    expect(screen.getByText('Delete Session')).toBeTruthy()
  })

  it('renders parsed import warnings and summary', () => {
    render(
      <ProviderSection
        {...buildProps({
          aistudioFileName: 'aistudio.har',
          aistudioImport: {
            sourceType: 'har',
            session: {
              endpoint: 'https://alkalimakersuite-pa.clients6.google.com',
              x_browser_validation: 'validation',
              x_client_data: 'client-data',
              x_goog_authuser: '0',
            },
            warnings: ['缺少 cookie/authorization，verify 很可能失敗。'],
          },
        })}
      />,
    )

    expect(screen.getByText(/來源：/)).toBeTruthy()
    expect(screen.getByText('缺少 cookie/authorization，verify 很可能失敗。')).toBeTruthy()
    expect(screen.getByText('aistudio.har')).toBeTruthy()
  })

  it('shows AI Studio RPC as selectable provider in oauth tab', () => {
    render(
      <ProviderSection
        {...buildProps({
          providerTab: 'oauth',
          form: {
            ...buildForm(),
            provider_type: 'oauth',
            provider_id: 'aistudio-rpc',
            model_id: 'aistudio-rpc/gemini-3-pro-preview',
            gemini_model: 'gemini-3-pro-preview',
          },
        })}
      />,
    )

    expect(screen.getByText('AI Studio RPC (Direct / Test)')).toBeTruthy()
    expect(screen.getByText('Use Selected Provider')).toBeTruthy()
  })

  it('shows Anthropic setup-token provider option in oauth tab', () => {
    render(
      <ProviderSection
        {...buildProps({
          providerTab: 'oauth',
          form: {
            ...buildForm(),
            provider_type: 'oauth',
            provider_id: 'anthropic-setup-token',
            model_id: 'anthropic-setup-token/claude-sonnet-4-6',
            gemini_model: 'claude-sonnet-4-6',
          },
        })}
      />,
    )

    expect(screen.getByText('Anthropic Setup Token (Claude Subscription)')).toBeTruthy()
  })

  it('renders Anthropic import UI in add-account modal', () => {
    render(
      <ProviderSection
        {...buildProps({
          providerTab: 'oauth',
          addAccountOpen: true,
          form: {
            ...buildForm(),
            provider_type: 'oauth',
            provider_id: 'anthropic-setup-token',
            model_id: 'anthropic-setup-token/claude-sonnet-4-6',
            gemini_model: 'claude-sonnet-4-6',
          },
        })}
      />,
    )

    expect(screen.getByText('新增 Anthropic Setup Token')).toBeTruthy()
    expect(screen.getByText('Import Setup Token')).toBeTruthy()
    expect(screen.getByText('Import from Environment')).toBeTruthy()
  })

  it('renders second/third fallback selectors', () => {
    render(
      <ProviderSection
        {...buildProps({
          form: {
            ...buildForm(),
            fallback_model_enabled: true,
            fallback_model_id: 'gemini-api-key/gemini-2.5-pro',
            fallback_model: 'gemini-2.5-pro',
          },
          modelOptions: [
            { value: 'gemini-api-key/gemini-2.5-pro', label: 'Gemini 2.5 Pro' },
            { value: 'gemini-api-key/gemini-2.0-flash', label: 'Gemini 2.0 Flash' },
          ],
        })}
      />,
    )

    expect(screen.getByText('第二備用模型（可選）')).toBeTruthy()
    expect(screen.getByText('第三備用模型（可選）')).toBeTruthy()
    expect(screen.getByText(/備用模型 #1 → 備用模型 #2 → 備用模型 #3 → AI Studio fallback/)).toBeTruthy()
  })
})
