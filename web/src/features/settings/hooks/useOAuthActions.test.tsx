import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../api'
import { useOAuthActions } from './useOAuthActions'

vi.mock('../../../api', () => ({
  completeGeminiOAuth: vi.fn(),
  getOAuthPool: vi.fn(),
  getOAuthStatus: vi.fn(),
  getProviderStatus: vi.fn(),
  importAnthropicSetupToken: vi.fn(),
  importAnthropicSetupTokenFromEnv: vi.fn(),
  importGeminiCLIToken: vi.fn(),
  importPastedToken: vi.fn(),
  removeOAuthPoolAccount: vi.fn(),
  startGeminiOAuth: vi.fn(),
  verifyOAuthPool: vi.fn(),
}))

describe('useOAuthActions', () => {
  const setMessage = vi.fn()
  const setStatus = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getOAuthStatus).mockResolvedValue({ authenticated: false })
    vi.mocked(api.getProviderStatus).mockResolvedValue({
      configured: true,
      provider_name: 'pool',
      model: 'claude-sonnet-4-6',
    })
    vi.mocked(api.getOAuthPool).mockResolvedValue([])
  })

  it('calls anthropic import-token API with setup token', async () => {
    vi.mocked(api.importAnthropicSetupToken).mockResolvedValue({
      email: 'anthropic:setup_token_deadbeef',
      provider_id: 'anthropic-setup-token',
      status: 'ok',
    })

    const { result } = renderHook(() => useOAuthActions({ setMessage, setStatus }))
    act(() => {
      result.current.setAnthropicSetupToken('sk-ant-oat01-test-token')
    })

    await act(async () => {
      await result.current.handleImportAnthropicToken()
    })

    expect(api.importAnthropicSetupToken).toHaveBeenCalledWith('sk-ant-oat01-test-token')
  })

  it('calls anthropic import-env API', async () => {
    vi.mocked(api.importAnthropicSetupTokenFromEnv).mockResolvedValue({
      email: 'anthropic:setup_token_deadbeef',
      provider_id: 'anthropic-setup-token',
      status: 'ok',
    })

    const { result } = renderHook(() => useOAuthActions({ setMessage, setStatus }))

    await act(async () => {
      await result.current.handleImportAnthropicEnv()
    })

    expect(api.importAnthropicSetupTokenFromEnv).toHaveBeenCalledTimes(1)
  })
})
