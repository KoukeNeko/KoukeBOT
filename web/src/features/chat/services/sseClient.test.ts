import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { streamMultipartChat, streamTextChat } from './sseClient'
import type { ChatStreamEvent } from '../types/stream'

class FakeEventSource {
  static instances: FakeEventSource[] = []

  readonly url: string
  readonly close = vi.fn()
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  private listeners = new Map<string, Array<(event: Event) => void>>()

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, listener: (event: Event) => void) {
    const existing = this.listeners.get(type) ?? []
    existing.push(listener)
    this.listeners.set(type, existing)
  }

  removeEventListener(type: string, listener: (event: Event) => void) {
    const existing = this.listeners.get(type) ?? []
    this.listeners.set(
      type,
      existing.filter((entry) => entry !== listener),
    )
  }

  emitMessage(data: string) {
    this.onmessage?.(new MessageEvent('message', { data }))
  }

  emit(type: 'tool' | 'done' | 'error', data = '') {
    const listeners = this.listeners.get(type) ?? []
    const event = type === 'tool' ? new MessageEvent(type, { data }) : new Event(type)
    for (const listener of listeners) {
      listener(event)
    }
  }
}

describe('sseClient', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('parses text stream message/tool/done events', () => {
    const events: ChatStreamEvent[] = []

    const stop = streamTextChat(
      {
        personaId: 'persona-1',
        message: 'hello world',
        channelId: 'channel-1',
        conversationId: 'conv-1',
      },
      (event) => events.push(event),
    )

    const source = FakeEventSource.instances[0]
    expect(source.url).toContain('/api/chat/stream?')

    source.emitMessage('line1\\nline2')
    source.emit('tool', JSON.stringify({ status: 'running', name: 'search' }))
    source.emit('done')

    expect(events).toEqual([
      { type: 'message', content: 'line1\nline2' },
      { type: 'tool', payload: { status: 'running', name: 'search' } },
      { type: 'done' },
    ])
    expect(source.close).toHaveBeenCalledTimes(1)

    stop()
    expect(source.close).toHaveBeenCalledTimes(2)
  })

  it('parses multipart stream events from fetch body', async () => {
    const events: ChatStreamEvent[] = []
    const payload = [
      'data: hello\\nworld\n',
      'event: tool\n',
      'data: {"status":"running","name":"search"}\n',
      'event: done\n',
      'data: {}\n',
    ].join('')

    const read = vi
      .fn()
      .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode(payload) })
      .mockResolvedValueOnce({ done: true, value: undefined })

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        statusText: 'OK',
        body: { getReader: () => ({ read }) },
      }),
    )

    await streamMultipartChat(new FormData(), (event) => events.push(event))

    expect(fetch).toHaveBeenCalledWith(
      '/api/chat/stream',
      expect.objectContaining({ method: 'POST' }),
    )
    expect(events).toEqual([
      { type: 'message', content: 'hello\nworld' },
      { type: 'tool', payload: { status: 'running', name: 'search' } },
      { type: 'done' },
    ])
  })
})
