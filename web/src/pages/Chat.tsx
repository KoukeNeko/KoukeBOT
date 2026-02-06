import { useEffect, useRef, useState } from 'react'
import { listPersonas, sendChat } from '../api'
import type { PersonaSummary } from '../types'

interface Message {
  role: 'user' | 'assistant'
  content: string
}

export default function Chat() {
  const [personas, setPersonas] = useState<PersonaSummary[]>([])
  const [personaId, setPersonaId] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    listPersonas().then((list) => {
      setPersonas(list)
      if (list.length > 0 && !personaId) {
        setPersonaId(list[0].id)
      }
    })
  }, [])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const handleSend = async () => {
    const text = input.trim()
    if (!text || !personaId || sending) return

    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: text }])
    setSending(true)

    try {
      const res = await sendChat(personaId, text)
      setMessages((prev) => [...prev, { role: 'assistant', content: res.response }])
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

  return (
    <div className="chat-container">
      <div className="chat-header">
        <h2>Chat Test</h2>
        <select value={personaId} onChange={(e) => setPersonaId(e.target.value)}>
          {personas.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name} ({p.id})
            </option>
          ))}
        </select>
      </div>

      <div className="chat-messages">
        {messages.length === 0 && (
          <p className="meta" style={{ textAlign: 'center', marginTop: '2rem' }}>
            Send a message to start chatting.
          </p>
        )}
        {messages.map((m, i) => (
          <div key={i} className={`chat-bubble ${m.role}`}>
            <pre>{m.content}</pre>
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
    </div>
  )
}
