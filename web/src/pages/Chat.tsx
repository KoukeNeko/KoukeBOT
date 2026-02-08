import { useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { listPersonas, listConversations, getConversation, deleteConversation } from '../api'
import type { PersonaSummary, ConversationSummary, Message } from '../types'
import { SidebarList, SidebarItem } from '../components/SidebarList'

export default function Chat() {
  const [personas, setPersonas] = useState<PersonaSummary[]>([])
  const [conversations, setConversations] = useState<ConversationSummary[]>([])
  const [selectedConv, setSelectedConv] = useState<{ channelId: string; personaId: string } | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [showNewConvModal, setShowNewConvModal] = useState(false)
  const [newConvPersonaId, setNewConvPersonaId] = useState('')
  const [selectedImages, setSelectedImages] = useState<File[]>([])
  const [expandedRawData, setExpandedRawData] = useState<Set<number>>(new Set())
  const bottomRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

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
        display_text: m.display_text,
        raw_llm_input: m.raw_llm_input,
        raw_llm_output: m.raw_llm_output,
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
    if ((!text && selectedImages.length === 0) || !selectedConv || sending) return

    setInput('')
    const images = selectedImages
    setSelectedImages([])

    const userMsg: Message = { role: 'user', content: text || '(含圖片)' }
    setMessages((prev) => [...prev, userMsg])
    setSending(true)

    // Add empty assistant message for streaming
    const assistantMsgIndex = messages.length + 1
    setMessages((prev) => [...prev, { role: 'assistant', content: '' }])

    try {
      let eventSource: EventSource

      if (images.length > 0) {
        // Use multipart/form-data for images
        const formData = new FormData()
        formData.append('persona_id', selectedConv.personaId)
        formData.append('message', text || '這張圖片顯示了什麼？')
        formData.append('channel_id', selectedConv.channelId)
        images.forEach((img) => formData.append('images', img))

        // Upload via POST, then get response stream URL
        const uploadResp = await fetch('/api/chat/stream', {
          method: 'POST',
          body: formData,
        })

        if (!uploadResp.ok) {
          throw new Error(`Upload failed: ${uploadResp.statusText}`)
        }

        // Read SSE stream from response body
        const reader = uploadResp.body?.getReader()
        if (!reader) throw new Error('No response body')

        const decoder = new TextDecoder()
        let accumulatedContent = ''

        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          const chunk = decoder.decode(value, { stream: true })
          const lines = chunk.split('\n')

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              const data = line.slice(6).replace(/\\n/g, '\n')
              accumulatedContent += data

              setMessages((prev) => {
                const updated = [...prev]
                updated[assistantMsgIndex] = {
                  role: 'assistant',
                  content: accumulatedContent,
                }
                return updated
              })
            } else if (line.startsWith('event: done')) {
              setSending(false)
              loadConversations()
              return
            } else if (line.startsWith('event: error')) {
              throw new Error('Stream error')
            }
          }
        }

        setSending(false)
        loadConversations()
      } else {
        // Use query params for text-only (original approach)
        const params = new URLSearchParams({
          persona_id: selectedConv.personaId,
          message: text,
          channel_id: selectedConv.channelId,
        })

        eventSource = new EventSource(`/api/chat/stream?${params}`)
        let accumulatedContent = ''

        eventSource.onmessage = (event) => {
          const chunk = event.data
          accumulatedContent += chunk.replace(/\\n/g, '\n')

          setMessages((prev) => {
            const updated = [...prev]
            updated[assistantMsgIndex] = {
              role: 'assistant',
              content: accumulatedContent,
            }
            return updated
          })
        }

        eventSource.addEventListener('done', () => {
          eventSource.close()
          setSending(false)
          loadConversations()
        })

        eventSource.addEventListener('error', (err) => {
          console.error('SSE error:', err)
          eventSource.close()
          setMessages((prev) => {
            const updated = [...prev]
            updated[assistantMsgIndex] = {
              role: 'assistant',
              content: accumulatedContent || 'Error: Stream connection failed',
            }
            return updated
          })
          setSending(false)
        })
      }

    } catch (e) {
      setMessages((prev) => [
        ...prev.slice(0, -1),
        { role: 'assistant', content: `Error: ${e instanceof Error ? e.message : 'Unknown'}` },
      ])
      setSending(false)
    }
  }

  const handleReply = (content: string) => {
    const quote = content.split('\n').map(line => `> ${line}`).join('\n')
    setInput((prev) => (prev ? `${prev}\n\n${quote}\n` : `${quote}\n`))
    // Focus the textarea after a short delay to ensure state update
    setTimeout(() => {
      const textarea = document.querySelector('.chat-input textarea') as HTMLTextAreaElement
      textarea?.focus()
    }, 0)
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

  const getConvSource = (channelId: string) => {
    return channelId === 'web-test' ? 'Web' : 'Discord'
  }

  const getConvSourceBadge = (channelId: string) => {
    const source = getConvSource(channelId)
    const color = source === 'Web' ? '#5865F2' : '#5865F2'
    return { source, color }
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
        {conversations.map((conv) => {
          const { source, color } = getConvSourceBadge(conv.channel_id)
          return (
            <SidebarItem
              key={conv.id}
              active={selectedConv?.channelId === conv.channel_id && selectedConv?.personaId === conv.persona_id}
              onClick={() => handleSelectConv(conv.channel_id, conv.persona_id)}
              title={conv.persona_name}
              subtitle={
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', gap: '0.5rem' }}>
                  <span
                    style={{
                      fontSize: '0.65rem',
                      padding: '1px 5px',
                      borderRadius: '4px',
                      background: color,
                      color: 'white',
                      fontWeight: 500,
                      opacity: 0.9,
                      whiteSpace: 'nowrap',
                      flexShrink: 0
                    }}
                  >
                    {source}
                  </span>
                  <span style={{ fontSize: '0.7rem', color: '#666', whiteSpace: 'nowrap', flexShrink: 0 }}>
                    {formatRelativeTime(conv.last_timestamp)}
                  </span>
                </div>
              }
              description={conv.last_message || '無訊息'}
            >
              <button
                className="btn-icon"
                title="刪除對話"
                onClick={(e) => handleDeleteConv(conv.channel_id, conv.persona_id, e)}
                style={{ padding: '4px', opacity: 0.6, marginLeft: 'auto' }}
              >
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <polyline points="3 6 5 6 21 6"></polyline>
                  <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                </svg>
              </button>
            </SidebarItem>
          )
        })}
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
                                {messages.map((m, i) => {
                                  const displayContent = m.display_text || m.content
                                  const hasRawData = m.role === 'assistant' && (m.raw_llm_input || m.raw_llm_output)
                                  const isExpanded = expandedRawData.has(i)

                                  return (
                                    <div key={i} className={`chat-bubble ${m.role}`}>
                                      {/* Tool calls indicator */}
                                      {m.tool_calls && m.tool_calls.length > 0 && (
                                        <div style={{
                                          background: 'rgba(66, 133, 244, 0.1)',
                                          borderLeft: '3px solid #4285f4',
                                          padding: '0.5rem',
                                          marginBottom: '0.5rem',
                                          borderRadius: '4px',
                                          fontSize: '0.85rem'
                                        }}>
                                          🔍 {m.tool_calls.map(call => {
                                            if (call.name === 'web_search') {
                                              return `正在搜尋: ${call.input.query || '...'}`
                                            }
                                            return `使用工具: ${call.name}`
                                          }).join(', ')}
                                        </div>
                                      )}

                                      {/* Message content */}
                                      {displayContent && (
                                        <div className="markdown-content">
                                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{displayContent}</ReactMarkdown>
                                        </div>
                                      )}

                                      {/* Reply Button */}
                                      <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '0.25rem', opacity: 0.6 }}>
                                        <button
                                          onClick={() => handleReply(displayContent || '')}
                                          className="btn-icon"
                                          title="引用回覆"
                                          style={{ padding: '2px', fontSize: '0.75rem', color: '#ccc' }}
                                        >
                                          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                            <polyline points="9 17 4 12 9 7"></polyline>
                                            <path d="M20 18v-2a4 4 0 0 0-4-4H4"></path>
                                          </svg>
                                        </button>
                                      </div>

                                      {/* Raw LLM Data Toggle (only for assistant messages with raw data) */}
                                      {hasRawData && (
                                        <div style={{ marginTop: '0.75rem', borderTop: '1px solid rgba(255,255,255,0.08)', paddingTop: '0.5rem' }}>
                                          <button
                                            onClick={() => {
                                              const newSet = new Set(expandedRawData)
                                              if (isExpanded) {
                                                newSet.delete(i)
                                              } else {
                                                newSet.add(i)
                                              }
                                              setExpandedRawData(newSet)
                                            }}
                                            style={{
                                              padding: '4px 8px',
                                              fontSize: '0.75rem',
                                              background: 'rgba(255,255,255,0.05)',
                                              border: '1px solid rgba(255,255,255,0.1)',
                                              borderRadius: '4px',
                                              cursor: 'pointer',
                                              display: 'flex',
                                              alignItems: 'center',
                                              gap: '0.25rem',
                                              color: 'rgba(255,255,255,0.7)'
                                            }}
                                          >
                                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                                              {isExpanded ? (
                                                <polyline points="18 15 12 9 6 15"></polyline>
                                              ) : (
                                                <polyline points="6 9 12 15 18 9"></polyline>
                                              )}
                                            </svg>
                                            {isExpanded ? '隱藏原始資料' : '顯示原始資料'}
                                          </button>

                                          {isExpanded && (
                                            <div style={{ marginTop: '0.5rem', fontSize: '0.75rem' }}>
                                              {m.raw_llm_input && (
                                                <div style={{ marginBottom: '0.75rem' }}>
                                                  <div style={{
                                                    fontWeight: 600,
                                                    marginBottom: '0.25rem',
                                                    color: 'rgba(255,255,255,0.8)',
                                                    fontSize: '0.8rem'
                                                  }}>
                                                    📥 LLM 輸入 (完整提示詞)
                                                  </div>
                                                  <pre style={{
                                                    background: 'rgba(0,0,0,0.3)',
                                                    padding: '0.5rem',
                                                    borderRadius: '4px',
                                                    overflow: 'auto',
                                                    maxHeight: '300px',
                                                    fontSize: '0.7rem',
                                                    lineHeight: '1.4',
                                                    color: 'rgba(255,255,255,0.7)',
                                                    whiteSpace: 'pre-wrap',
                                                    wordBreak: 'break-word'
                                                  }}>
                                                    {m.raw_llm_input}
                                                  </pre>
                                                </div>
                                              )}
                                              {m.raw_llm_output && (
                                                <div>
                                                  <div style={{
                                                    fontWeight: 600,
                                                    marginBottom: '0.25rem',
                                                    color: 'rgba(255,255,255,0.8)',
                                                    fontSize: '0.8rem'
                                                  }}>
                                                    📤 LLM 輸出 (原始回應)
                                                  </div>
                                                  <pre style={{
                                                    background: 'rgba(0,0,0,0.3)',
                                                    padding: '0.5rem',
                                                    borderRadius: '4px',
                                                    overflow: 'auto',
                                                    maxHeight: '300px',
                                                    fontSize: '0.7rem',
                                                    lineHeight: '1.4',
                                                    color: 'rgba(255,255,255,0.7)',
                                                    whiteSpace: 'pre-wrap',
                                                    wordBreak: 'break-word'
                                                  }}>
                                                    {m.raw_llm_output}
                                                  </pre>
                                                </div>
                                              )}
                                            </div>
                                          )}
                                        </div>
                                      )}
                                    </div>
                                  )
                                })}                                {sending && (
                                  <div style={{ padding: '0.5rem 1rem', opacity: 0.7 }}>
                                    <p className="meta" style={{ margin: 0, fontSize: '0.85rem' }}>Thinking...</p>
                                  </div>
                                )}
                                <div ref={bottomRef} />              </div>

              <div className="chat-input">
                {/* Image preview */}
                {selectedImages.length > 0 && (
                  <div style={{ display: 'flex', gap: '0.5rem', padding: '0.5rem', flexWrap: 'wrap', borderTop: '1px solid rgba(255,255,255,0.06)' }}>
                    {selectedImages.map((img, idx) => (
                      <div key={idx} style={{ position: 'relative', width: '80px', height: '80px' }}>
                        <img
                          src={URL.createObjectURL(img)}
                          alt={`preview-${idx}`}
                          style={{ width: '100%', height: '100%', objectFit: 'cover', borderRadius: '6px' }}
                        />
                        <button
                          onClick={() => setSelectedImages(prev => prev.filter((_, i) => i !== idx))}
                          style={{
                            position: 'absolute',
                            top: '2px',
                            right: '2px',
                            padding: '2px 6px',
                            fontSize: '12px',
                            background: 'rgba(0,0,0,0.7)',
                            border: 'none',
                            borderRadius: '4px',
                            color: 'white',
                            cursor: 'pointer',
                          }}
                        >
                          ✕
                        </button>
                      </div>
                    ))}
                  </div>
                )}
                <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end' }}>
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept="image/*"
                    multiple
                    style={{ display: 'none' }}
                    onChange={(e) => {
                      if (e.target.files) {
                        setSelectedImages(prev => [...prev, ...Array.from(e.target.files!)])
                      }
                    }}
                  />
                  <button
                    onClick={() => fileInputRef.current?.click()}
                    disabled={sending}
                    style={{ padding: '8px 12px', flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                    title="上傳圖片"
                  >
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />
                    </svg>
                  </button>
                  <textarea
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyDown={handleKeyDown}
                    placeholder="Type a message... (Enter to send, Shift+Enter for newline)"
                    disabled={sending}
                    rows={2}
                    style={{ flex: 1 }}
                  />
                  <button onClick={handleSend} disabled={sending || (!input.trim() && selectedImages.length === 0)} style={{ flexShrink: 0 }}>
                    Send
                  </button>
                </div>
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