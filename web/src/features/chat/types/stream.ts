export type ToolEventPayload = {
  status: string
  name: string
  input?: Record<string, unknown>
  error?: string
}

export type ChatStreamEvent =
  | { type: 'message'; content: string }
  | { type: 'tool'; payload: ToolEventPayload }
  | { type: 'done' }
  | { type: 'error'; message: string }
