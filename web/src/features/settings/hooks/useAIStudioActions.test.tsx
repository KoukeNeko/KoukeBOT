import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../api'
import { useAIStudioActions } from './useAIStudioActions'

vi.mock('../../../api', () => ({
  deleteAIStudioSession: vi.fn(),
  getAIStudioSessionStatus: vi.fn(),
  putAIStudioSession: vi.fn(),
  verifyAIStudioSession: vi.fn(),
}))

describe('useAIStudioActions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getAIStudioSessionStatus).mockResolvedValue({ configured: false })
    vi.mocked(api.putAIStudioSession).mockResolvedValue({ status: 'saved', configured: true })
  })

  function makeMockFile(content: string, name = 'session.json'): File {
    return {
      name,
      text: async () => content,
    } as unknown as File
  }

  it('applies session only after verify succeeds', async () => {
    vi.mocked(api.verifyAIStudioSession).mockResolvedValue({ verified: true, model: 'gemini-2.5-pro' })

    const { result } = renderHook(() => useAIStudioActions())

    const file = makeMockFile(
      JSON.stringify({
        endpoint: 'https://alkalimakersuite-pa.clients6.google.com',
        api_key: 'api-key',
        x_browser_validation: 'validation',
        x_client_data: 'client',
        x_goog_authuser: '0',
        cookie: 'SID=abc',
      }),
    )

    await act(async () => {
      await result.current.parseUploadFile(file)
    })
    await act(async () => {
      await result.current.uploadVerifyApply()
    })

    expect(api.verifyAIStudioSession).toHaveBeenCalledTimes(1)
    expect(api.putAIStudioSession).toHaveBeenCalledTimes(1)
  })

  it('does not apply session when verify fails', async () => {
    vi.mocked(api.verifyAIStudioSession).mockResolvedValue({
      verified: false,
      model: 'gemini-2.5-pro',
      error: 'unauthorized',
    })

    const { result } = renderHook(() => useAIStudioActions())
    const file = makeMockFile(
      JSON.stringify({
        endpoint: 'https://alkalimakersuite-pa.clients6.google.com',
        api_key: 'api-key',
        x_browser_validation: 'validation',
        x_client_data: 'client',
        x_goog_authuser: '0',
        cookie: 'SID=abc',
      }),
    )

    await act(async () => {
      await result.current.parseUploadFile(file)
    })
    await act(async () => {
      await result.current.uploadVerifyApply()
    })

    expect(api.verifyAIStudioSession).toHaveBeenCalledTimes(1)
    expect(api.putAIStudioSession).not.toHaveBeenCalled()
  })
})
