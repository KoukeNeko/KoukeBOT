import React, { useEffect, useState } from 'react'
import Editor from '@monaco-editor/react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useIsMobile } from '../hooks/useMediaQuery'

interface EditorModalProps {
  title: string
  language: string
  value: string
  onChange: (value: string | undefined) => void
  onClose: () => void
  theme?: string
  hint?: React.ReactNode
}

export default function EditorModal({
  title,
  language,
  value,
  onChange,
  onClose,
  theme = 'vs-dark',
  hint,
}: EditorModalProps) {
  const isMobile = useIsMobile()
  const [activeTab, setActiveTab] = useState<'editor' | 'preview'>('editor')

  // Close on Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  const canPreview = language === 'markdown' || language === 'yaml'
  const showPreviewSideBySide = canPreview && !isMobile

  return (
    <div className="modal-overlay">
      <div className="modal-content">
        <div className="modal-header">
          <div>
            <h3>{title}</h3>
            {hint && <div className="modal-hint">{hint}</div>}
          </div>
          <button onClick={onClose} className="close-btn">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          </button>
        </div>

        {isMobile && canPreview && (
          <div className="provider-tabs" style={{ background: '#1e1e1e' }}>
            <button 
              className={`provider-tab ${activeTab === 'editor' ? 'active' : ''}`}
              onClick={() => setActiveTab('editor')}
            >
              Editor
            </button>
            <button 
              className={`provider-tab ${activeTab === 'preview' ? 'active' : ''}`}
              onClick={() => setActiveTab('preview')}
            >
              Preview
            </button>
          </div>
        )}

        <div className={`modal-body ${showPreviewSideBySide ? 'modal-split-view' : ''}`}>
          {(!isMobile || activeTab === 'editor') && (
            <div className="modal-editor-pane">
              <Editor
                theme={theme}
                language={language}
                value={value}
                onChange={onChange}
                options={{
                  minimap: { enabled: false },
                  fontSize: 14,
                  lineNumbers: 'on',
                  scrollBeyondLastLine: false,
                  wordWrap: 'on',
                  padding: { top: 8, bottom: 8 },
                  automaticLayout: true,
                }}
              />
            </div>
          )}
          
          {(showPreviewSideBySide || (isMobile && activeTab === 'preview' && canPreview)) && (
            <div className="modal-preview-pane">
              <div className="markdown-preview">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {value}
                </ReactMarkdown>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}