import { useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { listPersonas, sendChat, listConversations, getConversation, deleteConversation } from '../api'
import type { PersonaSummary, ConversationSummary } from '../types'
import { SidebarList, SidebarItem } from '../components/SidebarList'

interface Message {
  role: 'user' | 'assistant'
  content: string
  timestamp?: string
}

export default function Chat() {
  const [personas, setPersonas] = useState<PersonaSummary[]>([])
  const [conversations, setConversations] = useState<ConversationSummary[]>([])
  const [selectedConv, setSelectedConv] = useState<{ channelId: string; personaId: string } | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [showNewConvModal, setShowNewConvModal] = useState(false)
  const [newConvPersonaId, setNewConvPersonaId] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)

  // Load personas and conversations on mount
  useEffect(() => {
    loadPersonas()
    loadConversations()
  }, [])

  // Load selected conversation messages
  useEffect(() => {
    if (selectedConv) {
      loadConversationMessages(selectedConv.channelId, selectedConv.personaId)
    }
  }, [selectedConv])

  // Auto-scroll to bottom when messages change
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const loadPersonas = async () => {
    try {
      const list = await listPersonas()
      setPersonas(list)
      if (list.length > 0 && !newConvPersonaId) {
        setNewConvPersonaId(list[0].id)
      }
    } catch (e) {
      console.error('Failed to load personas:', e)
    }
  }

  const loadConversations = async () => {
    try {
      const list = await listConversations()
      setConversations(list)
    } catch (e) {
      console.error('Failed to load conversations:', e)
    }
  }

  const loadConversationMessages = async (channelId: string, personaId: string) => {
    try {
      const conv = await getConversation(channelId, personaId)
      setMessages(conv.messages.map((m) => ({
        role: m.role as 'user' | 'assistant',
        content: m.content,
        timestamp: m.timestamp,
      })))
    } catch (e) {
      console.error('Failed to load conversation:', e)
      setMessages([])
    }
  }

  const handleSelectConv = (channelId: string, personaId: string) => {
    setSelectedConv({ channelId, personaId })
  }

  const handleNewConv = () => {
    if (!newConvPersonaId) return
    const channelId = 'web-test'
    setSelectedConv({ channelId, personaId: newConvPersonaId })
    setMessages([])
    setShowNewConvModal(false)
    // Don't reload conversations yet - wait until first message is sent
  }

  const handleDeleteConv = async (channelId: string, personaId: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (!confirm('確定要刪除此對話？')) return

    try {
      await deleteConversation(channelId, personaId)
      loadConversations()
      if (selectedConv?.channelId === channelId && selectedConv?.personaId === personaId) {
        setSelectedConv(null)
        setMessages([])
      }
    } catch (e) {
      console.error('Failed to delete conversation:', e)
    }
  }

  const handleSend = async () => {
    const text = input.trim()
    if (!text || !selectedConv || sending) return

    setInput('')
    const userMsg: Message = { role: 'user', content: text }
    setMessages((prev) => [...prev, userMsg])
    setSending(true)

    try {
      const res = await sendChat(selectedConv.personaId, text, selectedConv.channelId)
      setMessages((prev) => [...prev, { role: 'assistant', content: res.response }])
      // Reload conversation list to show new/updated conversation
      await loadConversations()
    } catch (e) {
      setMessages((prev) => [
        ...prev,
        { role: 'assistant', content: `Error: ${e instanceof Error ? e.message : 'Unknown'}` },
      ])
    } finally {
      setSending(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const getPersonaName = (personaId: string) => {
    const persona = personas.find((p) => p.id === personaId)
    return persona ? persona.name : personaId
  }

  const formatRelativeTime = (timestamp: string) => {
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
  }

  return (
    <div className="persona-layout">
      {/* Left sidebar: Conversation list */}
      <SidebarList 
        title="對話" 
        onReload={() => {
          loadPersonas()
          loadConversations()
        }}
      >
        <div style={{ padding: '0.75rem 1.25rem' }}>
          <button 
            style={{ width: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '0.5rem' }}
            onClick={() => setShowNewConvModal(true)}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <line x1="12" y1="5" x2="12" y2="19"></line>
              <line x1="5" y1="12" x2="19" y2="12"></line>
            </svg>
            新對話
          </button>
        </div>

        {conversations.length === 0 && (
          <p className="meta" style={{ padding: '1rem', textAlign: 'center' }}>
            尚無對話記錄
          </p>
        )}
        {conversations.map((conv) => (
          <SidebarItem
            key={conv.id}
            active={selectedConv?.channelId === conv.channel_id && selectedConv?.personaId === conv.persona_id}
            onClick={() => handleSelectConv(conv.channel_id, conv.persona_id)}
            title={conv.persona_name}
            description={conv.last_message || '無訊息'}
            subtitle={formatRelativeTime(conv.last_timestamp)}
          >
            <button
              className="btn-icon"
              title="刪除對話"
              onClick={(e) => handleDeleteConv(conv.channel_id, conv.persona_id, e)}
              style={{ padding: '4px', opacity: 0.6 }}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
            </button>
          </SidebarItem>
        ))}
      </SidebarList>

      {/* Right panel: Chat messages */}
      <div className="persona-detail-col" style={{ padding: 0, display: 'flex', flexDirection: 'column' }}>
        <div className="chat-container">
          {!selectedConv ? (
            <div className="empty-state" style={{ flexDirection: 'column', gap: '1rem' }}>
              <p>選擇一個對話或建立新對話開始聊天</p>
              <button onClick={() => setShowNewConvModal(true)}>
                建立新對話
              </button>
            </div>
          ) : (
            <>
              <div className="chat-header">
                <div>
                  <h2>{getPersonaName(selectedConv.personaId)}</h2>
                  <div className="meta">Channel: {selectedConv.channelId}</div>
                </div>
              </div>

              <div className="chat-messages">
                {messages.length === 0 && (
                  <p className="meta" style={{ textAlign: 'center', marginTop: '2rem' }}>
                    發送訊息開始對話
                  </p>
                )}
                {messages.map((m, i) => (
                  <div key={i} className={`chat-bubble ${m.role}`}>
                    <div className="markdown-content">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.content}</ReactMarkdown>
                    </div>
                  </div>
                ))}
                {sending && (
                  <div className="chat-bubble assistant">
                    <p className="meta">Thinking...</p>
                  </div>
                )}
                <div ref={bottomRef} />
              </div>

              <div className="chat-input">
                <textarea
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Type a message... (Enter to send, Shift+Enter for newline)"
                  disabled={sending}
                  rows={2}
                />
                <button onClick={handleSend} disabled={sending || !input.trim()}>
                  Send
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      {/* New conversation modal */}
      {showNewConvModal && (
        <div className="modal-overlay" onClick={() => setShowNewConvModal(false)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: '450px', height: 'auto', maxHeight: '85vh' }}>
            <div className="modal-header">
              <h3>建立新對話</h3>
              <button onClick={() => setShowNewConvModal(false)} className="close-btn">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
              </button>
            </div>
            <div className="modal-body" style={{ padding: '1.25rem', overflowY: 'auto' }}>
              <p className="meta" style={{ marginBottom: '1.25rem' }}>請選擇一個 Persona 開始新的對話記錄。</p>
              
              <div className="persona-selection-list" style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                {personas.map((p) => (
                  <div
                    key={p.id}
                    className={`persona-list-item ${newConvPersonaId === p.id ? 'active' : ''}`}
                    onClick={() => setNewConvPersonaId(p.id)}
                    style={{ cursor: 'pointer', border: '1px solid rgba(255,255,255,0.06)', borderRadius: '10px' }}
                  >
                    <div className="persona-item-header">
                      <span className="persona-name">{p.name}</span>
                      <span className="persona-version">v{p.version}</span>
                    </div>
                    {p.description && (
                      <div className="persona-item-desc" style={{ WebkitLineClamp: 2, display: '-webkit-box', WebkitBoxOrient: 'vertical', whiteSpace: 'normal' }}>
                        {p.description}
                      </div>
                    )}
                  </div>
                ))}
              </div>

              <div style={{ display: 'flex', gap: '0.75rem', marginTop: '1.75rem', justifyContent: 'flex-end' }}>
                <button onClick={() => setShowNewConvModal(false)} className="btn-secondary">
                  取消
                </button>
                <button 
                  onClick={handleNewConv} 
                  disabled={!newConvPersonaId}
                  style={{ background: '#646cff', color: '#fff', borderColor: '#646cff' }}
                >
                  開始對話
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}