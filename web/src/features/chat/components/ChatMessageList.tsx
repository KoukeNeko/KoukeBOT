import type { RefObject } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import type { Message } from '../../../types'

type Props = {
  messages: Message[]
  sending: boolean
  onReply: (content: string) => void
  bottomRef: RefObject<HTMLDivElement | null>
  expandedRawData: Set<number>
  expandedToolUsage: Set<number>
  showToonRawInput: Set<number>
  setExpandedRawData: (next: Set<number>) => void
  setExpandedToolUsage: (next: Set<number>) => void
  setShowToonRawInput: (next: Set<number>) => void
}

export default function ChatMessageList({
  messages,
  sending,
  onReply,
  bottomRef,
  expandedRawData,
  expandedToolUsage,
  showToonRawInput,
  setExpandedRawData,
  setExpandedToolUsage,
  setShowToonRawInput,
}: Props) {
  return (
    <div className="chat-messages">
      {messages.length === 0 && (
        <p className="meta" style={{ textAlign: 'center', marginTop: '2rem' }}>
          發送訊息開始對話
        </p>
      )}

      {messages.map((message, index) => {
        const displayContent = message.display_text || message.content
        const hasRawData = message.role === 'assistant' && (message.raw_llm_input || message.raw_llm_output)
        const isRawExpanded = expandedRawData.has(index)
        const hasRawInputDualView = Boolean(message.raw_llm_input && message.raw_llm_input_human)
        const isToonInputView = showToonRawInput.has(index)
        const rawInputPreview = hasRawInputDualView && !isToonInputView
          ? message.raw_llm_input_human
          : message.raw_llm_input

        let mainContent = displayContent
        let toolUsageAnnotation = ''
        if (displayContent && displayContent.includes('\n---\n🔧')) {
          const parts = displayContent.split('\n---\n')
          mainContent = parts[0]
          toolUsageAnnotation = `---\n${parts.slice(1).join('\n---\n')}`
        }
        const hasToolUsage = toolUsageAnnotation.length > 0
        const isToolExpanded = expandedToolUsage.has(index)

        return (
          <div key={index} className={`chat-bubble ${message.role}`}>
            {message.tool_calls && message.tool_calls.length > 0 && (
              <div
                style={{
                  background: 'rgba(66, 133, 244, 0.1)',
                  borderLeft: '3px solid #4285f4',
                  padding: '0.5rem',
                  marginBottom: '0.5rem',
                  borderRadius: '4px',
                  fontSize: '0.85rem',
                }}
              >
                🔍 {message.tool_calls
                  .map((call) => {
                    if (call.name === 'web_search') {
                      const value = call.input.query
                      const query = typeof value === 'string' ? value : ''
                      return `正在搜尋: ${query || '...'}`
                    }
                    return `使用工具: ${call.name}`
                  })
                  .join(', ')}
              </div>
            )}

            {mainContent && (
              <div className="markdown-content">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{mainContent}</ReactMarkdown>
              </div>
            )}

            {hasToolUsage && (
              <div style={{ marginTop: '0.5rem', borderTop: '1px solid rgba(255,255,255,0.08)', paddingTop: '0.5rem' }}>
                <button
                  onClick={() => {
                    const next = new Set(expandedToolUsage)
                    if (isToolExpanded) next.delete(index)
                    else next.add(index)
                    setExpandedToolUsage(next)
                  }}
                  className="btn-secondary"
                  style={{ padding: '4px 8px', fontSize: '0.75rem', width: 'auto' }}
                >
                  {isToolExpanded ? '隱藏工具記錄' : '顯示工具記錄'}
                </button>

                {isToolExpanded && (
                  <div
                    style={{
                      marginTop: '0.5rem',
                      padding: '0.75rem',
                      background: 'rgba(100,149,237,0.05)',
                      borderRadius: '6px',
                      fontSize: '0.85rem',
                    }}
                  >
                    <div className="markdown-content">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{toolUsageAnnotation}</ReactMarkdown>
                    </div>
                  </div>
                )}
              </div>
            )}

            <button onClick={() => onReply(displayContent || '')} className="btn-icon reply-btn">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <polyline points="9 17 4 12 9 7" />
                <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
              </svg>
            </button>

            {hasRawData && (
              <div style={{ marginTop: '0.75rem', borderTop: '1px solid rgba(255,255,255,0.08)', paddingTop: '0.5rem' }}>
                <button
                  onClick={() => {
                    const next = new Set(expandedRawData)
                    if (isRawExpanded) next.delete(index)
                    else next.add(index)
                    setExpandedRawData(next)
                  }}
                  className="btn-secondary"
                  style={{ padding: '4px 8px', fontSize: '0.75rem', width: 'auto' }}
                >
                  {isRawExpanded ? '隱藏原始資料' : '顯示原始資料'}
                </button>

                {isRawExpanded && (
                  <div style={{ marginTop: '0.5rem', fontSize: '0.75rem' }}>
                    {message.raw_llm_input && (
                      <div style={{ marginBottom: '0.75rem' }}>
                        <div style={{ fontWeight: 600, marginBottom: '0.25rem' }}>📥 LLM 輸入</div>
                        {hasRawInputDualView && (
                          <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '0.4rem' }}>
                            <button
                              className="btn-secondary"
                              style={{ padding: '2px 8px', fontSize: '0.7rem', width: 'auto' }}
                              onClick={() => {
                                const next = new Set(showToonRawInput)
                                next.delete(index)
                                setShowToonRawInput(next)
                              }}
                              disabled={!isToonInputView}
                            >
                              Human
                            </button>
                            <button
                              className="btn-secondary"
                              style={{ padding: '2px 8px', fontSize: '0.7rem', width: 'auto' }}
                              onClick={() => {
                                const next = new Set(showToonRawInput)
                                next.add(index)
                                setShowToonRawInput(next)
                              }}
                              disabled={isToonInputView}
                            >
                              TOON
                            </button>
                          </div>
                        )}
                        <pre
                          style={{
                            background: 'rgba(0,0,0,0.3)',
                            padding: '0.5rem',
                            borderRadius: '4px',
                            overflow: 'auto',
                            maxHeight: '200px',
                            whiteSpace: 'pre-wrap',
                          }}
                        >
                          {rawInputPreview}
                        </pre>
                      </div>
                    )}
                    {message.raw_llm_output && (
                      <div>
                        <div style={{ fontWeight: 600, marginBottom: '0.25rem' }}>📤 LLM 輸出</div>
                        <pre
                          style={{
                            background: 'rgba(0,0,0,0.3)',
                            padding: '0.5rem',
                            borderRadius: '4px',
                            overflow: 'auto',
                            maxHeight: '200px',
                            whiteSpace: 'pre-wrap',
                          }}
                        >
                          {message.raw_llm_output}
                        </pre>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        )
      })}

      {sending && (
        <div style={{ padding: '0.5rem 1rem', opacity: 0.7 }}>
          <p className="meta" style={{ margin: 0, fontSize: '0.85rem' }}>
            Thinking...
          </p>
        </div>
      )}
      <div ref={bottomRef} />
    </div>
  )
}
