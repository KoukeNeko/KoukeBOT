import type { ChatStreamEvent, ToolEventPayload } from '../types/stream'

type StreamParams = {
  personaId: string
  message: string
  channelId: string
  conversationId: string
}

function parseToolPayload(raw: string): ToolEventPayload | null {
  try {
    return JSON.parse(raw) as ToolEventPayload
  } catch {
    return null
  }
}

function parseDataChunk(raw: string): string {
  return raw.replace(/\\n/g, '\n')
}

export function streamTextChat(params: StreamParams, onEvent: (event: ChatStreamEvent) => void): () => void {
  const query = new URLSearchParams({
    persona_id: params.personaId,
    message: params.message,
    channel_id: params.channelId,
    conversation_id: params.conversationId,
  })

  const source = new EventSource(`/api/chat/stream?${query.toString()}`)

  source.onmessage = (event) => {
    onEvent({ type: 'message', content: parseDataChunk(event.data) })
  }

  source.addEventListener('tool', (event: Event) => {
    const msg = event as MessageEvent<string>
    const payload = parseToolPayload(msg.data)
    if (!payload) return
    onEvent({ type: 'tool', payload })
  })

  source.addEventListener('done', () => {
    onEvent({ type: 'done' })
    source.close()
  })

  source.addEventListener('error', () => {
    onEvent({ type: 'error', message: 'Stream connection failed' })
    source.close()
  })

  return () => source.close()
}

export async function streamMultipartChat(formData: FormData, onEvent: (event: ChatStreamEvent) => void): Promise<void> {
  const response = await fetch('/api/chat/stream', {
    method: 'POST',
    body: formData,
  })

  if (!response.ok) {
    throw new Error(`Upload failed: ${response.statusText}`)
  }

  const reader = response.body?.getReader()
  if (!reader) {
    throw new Error('No response body')
  }

  const decoder = new TextDecoder()
  let buffer = ''
  let currentEvent = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break

    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''

    for (const line of lines) {
      if (line.startsWith('event: ')) {
        currentEvent = line.slice(7).trim()
        continue
      }

      if (!line.startsWith('data: ')) {
        continue
      }

      const data = line.slice(6)
      if (currentEvent === 'tool') {
        const payload = parseToolPayload(data)
        if (payload) {
          onEvent({ type: 'tool', payload })
        }
      } else if (currentEvent === 'done') {
        onEvent({ type: 'done' })
        return
      } else if (currentEvent === 'error') {
        onEvent({ type: 'error', message: 'Stream error' })
        return
      } else {
        onEvent({ type: 'message', content: parseDataChunk(data) })
      }

      currentEvent = ''
    }
  }
}
