import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useMediaQuery } from './useMediaQuery'

describe('useMediaQuery', () => {
  let matches = false
  let listeners = new Set<() => void>()

  beforeEach(() => {
    matches = false
    listeners = new Set()

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn((query: string): MediaQueryList => ({
        matches,
        media: query,
        onchange: null,
        addEventListener: (_type: string, listener: EventListenerOrEventListenerObject) => {
          listeners.add(listener as unknown as () => void)
        },
        removeEventListener: (_type: string, listener: EventListenerOrEventListenerObject) => {
          listeners.delete(listener as unknown as () => void)
        },
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn().mockReturnValue(true),
      })),
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('reflects current match value and updates on change', () => {
    const { result } = renderHook(() => useMediaQuery('(max-width: 768px)'))

    expect(result.current).toBe(false)

    act(() => {
      matches = true
      for (const listener of listeners) {
        listener()
      }
    })

    expect(result.current).toBe(true)
  })
})
