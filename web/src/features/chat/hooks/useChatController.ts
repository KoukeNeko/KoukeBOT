import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent, MouseEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import {
  createConversationSession,
  deleteConversationByID,
  getConversation,
  getConversationByID,
  listConversations,
  listPersonas,
} from '../../../api'
import { useIsMobile } from '../../../hooks/useMediaQuery'
import type { ConversationSummary, Message, PersonaSummary } from '../../../types'
import { streamMultipartChat, streamTextChat } from '../services/sseClient'
import type { ToolEventPayload } from '../types/stream'

type SelectedConversation = {
  conversationId: string
  channelId: string
  personaId: string
}

function toChatRole(role: string): 'user' | 'assistant' {
  return role === 'assistant' ? 'assistant' : 'user'
}

function toChatMessage(message: Message): Message {
  return {
    role: toChatRole(message.role),
    content: message.content,
    display_text: message.display_text,
    raw_llm_input: message.raw_llm_input,
    raw_llm_input_human: message.raw_llm_input_human,
    raw_llm_output: message.raw_llm_output,
    timestamp: message.timestamp,
    tool_calls: message.tool_calls,
    tool_results: message.tool_results,
  }
}

function toolStartMessage(payload: ToolEventPayload): string {
  const input = payload.input ?? {}
  const text = (key: string) => {
    const value = input[key]
    return typeof value === 'string' ? value : ''
  }

  if (payload.name === 'web_search') {
    const query = text('query')
    return query ? `🔍 正在搜尋：${query}` : '🔍 正在搜尋...'
  }
  if (payload.name === 'web_fetch') {
    const url = text('url')
    return url ? `📄 正在擷取網頁：${url}` : '📄 正在擷取網頁...'
  }
  if (payload.name === 'file_read') {
    const filename = text('filename')
    return filename ? `📖 正在讀取檔案：${filename}` : '📖 正在讀取檔案...'
  }
  if (payload.name === 'file_write') {
    const filename = text('filename')
    return filename ? `💾 正在寫入檔案：${filename}` : '💾 正在寫入檔案...'
  }
  if (payload.name === 'file_append') {
    const filename = text('filename')
    return filename ? `📝 正在追加內容到：${filename}` : '📝 正在追加內容...'
  }
  if (payload.name === 'session_status') {
    return '⏰ 正在取得時間資訊...'
  }

  return `🔧 正在使用工具：${payload.name}`
}

export function useChatController() {
  const navigate = useNavigate()
  const { id: urlPersonaId } = useParams<{ id: string }>()
  const isMobile = useIsMobile()

  const [personas, setPersonas] = useState<PersonaSummary[]>([])
  const [conversations, setConversations] = useState<ConversationSummary[]>([])
  const [selectedConv, setSelectedConv] = useState<SelectedConversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [showNewConvModal, setShowNewConvModal] = useState(false)
  const [newConvPersonaId, setNewConvPersonaId] = useState('')
  const [selectedImages, setSelectedImages] = useState<File[]>([])
  const [expandedRawData, setExpandedRawData] = useState<Set<number>>(new Set())
  const [expandedToolUsage, setExpandedToolUsage] = useState<Set<number>>(new Set())
  const [showToonRawInput, setShowToonRawInput] = useState<Set<number>>(new Set())

  const bottomRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const loadPersonas = useCallback(async () => {
    try {
      const list = await listPersonas()
      setPersonas(list)
      setNewConvPersonaId((prev) => {
        if (prev) return prev
        return list.length > 0 ? list[0].id : ''
      })
    } catch (error) {
      console.error('Failed to load personas:', error)
    }
  }, [])

  const loadConversations = useCallback(async () => {
    try {
      const list = await listConversations()
      setConversations(list)
    } catch (error) {
      console.error('Failed to load conversations:', error)
    }
  }, [])

  const loadConversationMessages = useCallback(async (conversationId: string) => {
    try {
      const conv = await getConversationByID(conversationId)
      setMessages(conv.messages.map((m) => toChatMessage(m)))
    } catch (error) {
      console.error('Failed to load conversation:', error)
      setMessages([])
    }
  }, [])

  const selectActiveConversation = useCallback(async (channelId: string, personaId: string) => {
    try {
      const conv = await getConversation(channelId, personaId)
      setSelectedConv({
        conversationId: conv.id,
        channelId: conv.channel_id,
        personaId: conv.persona_id,
      })
      setMessages(conv.messages.map((m) => toChatMessage(m)))
    } catch (error) {
      console.error('Failed to select active conversation:', error)
      setMessages([])
    }
  }, [])

  useEffect(() => {
    void loadPersonas()
    void loadConversations()
  }, [loadConversations, loadPersonas])

  useEffect(() => {
    if (!urlPersonaId) return
    if (selectedConv && selectedConv.personaId === urlPersonaId) return
    void selectActiveConversation('web-test', urlPersonaId)
  }, [selectedConv, selectActiveConversation, urlPersonaId])

  useEffect(() => {
    if (!selectedConv?.conversationId) return
    void loadConversationMessages(selectedConv.conversationId)
  }, [loadConversationMessages, selectedConv?.conversationId])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const handleSelectConv = useCallback((conv: ConversationSummary) => {
    setSelectedConv({ conversationId: conv.id, channelId: conv.channel_id, personaId: conv.persona_id })
    if (isMobile) {
      navigate(`/chat/${conv.persona_id}`)
    }
  }, [isMobile, navigate])

  const openNewConversationModal = useCallback(() => {
    if (selectedConv?.personaId) {
      setNewConvPersonaId(selectedConv.personaId)
    } else if (personas.length > 0) {
      setNewConvPersonaId(personas[0].id)
    }
    setShowNewConvModal(true)
  }, [personas, selectedConv?.personaId])

  const handleNewConv = useCallback(async () => {
    if (!newConvPersonaId) return

    try {
      const conv = await createConversationSession(newConvPersonaId, 'web-test')
      setSelectedConv({
        conversationId: conv.id,
        channelId: conv.channel_id,
        personaId: conv.persona_id,
      })
      setMessages([])
      setShowNewConvModal(false)
      await loadConversations()
      if (isMobile) {
        navigate(`/chat/${conv.persona_id}`)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error'
      console.error('Failed to create conversation:', error)
      window.alert(`建立新對話失敗：${message}`)
    }
  }, [isMobile, loadConversations, navigate, newConvPersonaId])

  const handleDeleteConv = useCallback(async (conv: ConversationSummary, event: MouseEvent) => {
    event.stopPropagation()
    if (!window.confirm('確定要刪除此對話？')) return

    try {
      await deleteConversationByID(conv.id)
      await loadConversations()
      if (selectedConv?.conversationId === conv.id) {
        setSelectedConv(null)
        setMessages([])
        if (isMobile) navigate('/chat')
      }
    } catch (error) {
      console.error('Failed to delete conversation:', error)
    }
  }, [isMobile, loadConversations, navigate, selectedConv?.conversationId])

  const appendAssistantText = useCallback((index: number, content: string) => {
    setMessages((prev) => {
      const updated = [...prev]
      updated[index] = { role: 'assistant', content }
      return updated
    })
  }, [])

  const handleSend = useCallback(async () => {
    const text = input.trim()
    if ((!text && selectedImages.length === 0) || !selectedConv || sending) return

    setInput('')
    const images = selectedImages
    setSelectedImages([])

    setMessages((prev) => [...prev, { role: 'user', content: text || '(含圖片)' }, { role: 'assistant', content: '' }])
    setSending(true)

    const assistantMsgIndex = messages.length + 1

    try {
      let accumulatedContent = ''

      if (images.length > 0) {
        const formData = new FormData()
        formData.append('persona_id', selectedConv.personaId)
        formData.append('message', text || '這張圖片顯示了什麼？')
        formData.append('channel_id', selectedConv.channelId)
        formData.append('conversation_id', selectedConv.conversationId)
        images.forEach((img) => formData.append('images', img))

        await streamMultipartChat(formData, (event) => {
          if (event.type === 'tool') {
            if (event.payload.status === 'tool_start') {
              appendAssistantText(assistantMsgIndex, toolStartMessage(event.payload))
            } else if (event.payload.status === 'tool_done') {
              accumulatedContent = ''
            }
            return
          }

          if (event.type === 'message') {
            accumulatedContent += event.content
            appendAssistantText(assistantMsgIndex, accumulatedContent)
            return
          }

          if (event.type === 'error') {
            throw new Error(event.message)
          }
        })
      } else {
        await new Promise<void>((resolve, reject) => {
          const close = streamTextChat(
            {
              personaId: selectedConv.personaId,
              message: text,
              channelId: selectedConv.channelId,
              conversationId: selectedConv.conversationId,
            },
            (event) => {
              if (event.type === 'tool') {
                if (event.payload.status === 'tool_start') {
                  appendAssistantText(assistantMsgIndex, toolStartMessage(event.payload))
                } else if (event.payload.status === 'tool_done') {
                  accumulatedContent = ''
                }
                return
              }

              if (event.type === 'message') {
                accumulatedContent += event.content
                appendAssistantText(assistantMsgIndex, accumulatedContent)
                return
              }

              if (event.type === 'done') {
                close()
                resolve()
                return
              }

              close()
              reject(new Error(event.message))
            },
          )
        })
      }

      setSending(false)
      await loadConversations()
    } catch (error) {
      setMessages((prev) => {
        const updated = [...prev]
        updated[assistantMsgIndex] = {
          role: 'assistant',
          content: `Error: ${error instanceof Error ? error.message : 'Unknown'}`,
        }
        return updated
      })
      setSending(false)
    }
  }, [
    appendAssistantText,
    input,
    loadConversations,
    messages.length,
    selectedConv,
    selectedImages,
    sending,
  ])

  const handleReply = useCallback((content: string) => {
    const quote = content.split('\n').map((line) => `> ${line}`).join('\n')
    setInput((prev) => (prev ? `${prev}\n\n${quote}\n` : `${quote}\n`))
    setTimeout(() => {
      const textarea = document.querySelector('.chat-input textarea') as HTMLTextAreaElement | null
      textarea?.focus()
    }, 0)
  }, [])

  const handleKeyDown = useCallback((event: KeyboardEvent) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void handleSend()
    }
  }, [handleSend])

  const closeChatForMobile = useCallback(() => {
    setSelectedConv(null)
    navigate('/chat')
  }, [navigate])

  const showList = !isMobile || !selectedConv
  const showChat = !isMobile || !!selectedConv

  const helpers = useMemo(() => ({
    getPersonaName: (personaId: string) => {
      const persona = personas.find((p) => p.id === personaId)
      return persona ? persona.name : personaId
    },
    formatRelativeTime: (timestamp: string) => {
      if (!timestamp) return ''
      const date = new Date(timestamp)
      const now = new Date()
      const diff = now.getTime() - date.getTime()
      const minutes = Math.floor(diff / 60000)
      const hours = Math.floor(diff / 3600000)
      const days = Math.floor(diff / 86400000)

      if (minutes < 1) return '剛剛'
      if (minutes < 60) return `${minutes} 分鐘前`
      if (hours < 24) return `${hours} 小時前`
      if (days < 7) return `${days} 天前`
      return date.toLocaleDateString('zh-TW')
    },
    getConvSourceBadge: (channelId: string) => {
      const source = channelId === 'web-test' ? 'Web' : 'Discord'
      return { source, color: '#5865F2' }
    },
  }), [personas])

  return {
    personas,
    conversations,
    selectedConv,
    messages,
    input,
    sending,
    showNewConvModal,
    newConvPersonaId,
    selectedImages,
    expandedRawData,
    expandedToolUsage,
    showToonRawInput,
    bottomRef,
    fileInputRef,
    showList,
    showChat,
    isMobile,
    helpers,
    setInput,
    setSelectedImages,
    setShowNewConvModal,
    setNewConvPersonaId,
    setExpandedRawData,
    setExpandedToolUsage,
    setShowToonRawInput,
    handleSelectConv,
    openNewConversationModal,
    handleNewConv,
    handleDeleteConv,
    handleSend,
    handleReply,
    handleKeyDown,
    closeChatForMobile,
    reloadSidebar: async () => {
      await Promise.all([loadPersonas(), loadConversations()])
    },
  }
}
