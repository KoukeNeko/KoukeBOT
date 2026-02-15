import type { ChatResponse, Conversation, ConversationSummary } from '../types'
import { BASE, fetchJSON } from './client'

export function sendChat(
  personaId: string,
  message: string,
  channelId = 'web-test',
  conversationId?: string,
): Promise<ChatResponse> {
  return fetchJSON(`${BASE}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      channel_id: channelId,
      persona_id: personaId,
      conversation_id: conversationId ?? '',
      message,
    }),
  })
}

export function listConversations(): Promise<ConversationSummary[]> {
  return fetchJSON(`${BASE}/conversations`)
}

export function createConversationSession(personaId: string, channelId = 'web-test'): Promise<Conversation> {
  return fetchJSON(`${BASE}/conversations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      persona_id: personaId,
      channel_id: channelId,
    }),
  })
}

export function getConversation(channelId: string, personaId: string): Promise<Conversation> {
  return fetchJSON(`${BASE}/conversations/${encodeURIComponent(channelId)}/${encodeURIComponent(personaId)}`)
}

export function getConversationByID(conversationId: string): Promise<Conversation> {
  return fetchJSON(`${BASE}/conversations/by-id/${encodeURIComponent(conversationId)}`)
}

export function deleteConversation(channelId: string, personaId: string): Promise<{ status: string }> {
  return fetchJSON(`${BASE}/conversations/${encodeURIComponent(channelId)}/${encodeURIComponent(personaId)}`, {
    method: 'DELETE',
  })
}

export function deleteConversationByID(conversationId: string): Promise<{ status: string; new_conversation_id?: string }> {
  return fetchJSON(`${BASE}/conversations/by-id/${encodeURIComponent(conversationId)}`, {
    method: 'DELETE',
  })
}
