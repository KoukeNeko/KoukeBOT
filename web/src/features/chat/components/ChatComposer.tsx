import type React from 'react'

type Props = {
  input: string
  sending: boolean
  selectedImages: File[]
  isMobile: boolean
  fileInputRef: React.RefObject<HTMLInputElement | null>
  onInputChange: (value: string) => void
  onKeyDown: (event: React.KeyboardEvent) => void
  onSend: () => void
  onImagesChange: (files: File[]) => void
}

export default function ChatComposer({
  input,
  sending,
  selectedImages,
  isMobile,
  fileInputRef,
  onInputChange,
  onKeyDown,
  onSend,
  onImagesChange,
}: Props) {
  return (
    <div className="chat-input">
      {selectedImages.length > 0 && (
        <div style={{ display: 'flex', gap: '0.5rem', padding: '0.5rem', flexWrap: 'wrap', borderTop: '1px solid rgba(255,255,255,0.06)' }}>
          {selectedImages.map((image, index) => (
            <div key={`${image.name}-${index}`} style={{ position: 'relative', width: '60px', height: '60px' }}>
              <img
                src={URL.createObjectURL(image)}
                alt=""
                style={{ width: '100%', height: '100%', objectFit: 'cover', borderRadius: '6px' }}
              />
              <button
                onClick={() => onImagesChange(selectedImages.filter((_, i) => i !== index))}
                style={{
                  position: 'absolute',
                  top: '-4px',
                  right: '-4px',
                  background: '#000',
                  color: '#fff',
                  borderRadius: '50%',
                  border: 'none',
                  width: '20px',
                  height: '20px',
                  cursor: 'pointer',
                }}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}

      <div style={{ display: 'flex', gap: '0.5rem', padding: '0.75rem 1rem', alignItems: 'flex-end' }}>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*"
          multiple
          style={{ display: 'none' }}
          onChange={(event) => {
            if (!event.target.files) return
            onImagesChange([...selectedImages, ...Array.from(event.target.files)])
          }}
        />

        <button onClick={() => fileInputRef.current?.click()} disabled={sending} className="btn-icon">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />
          </svg>
        </button>

        <textarea
          value={input}
          onChange={(event) => onInputChange(event.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Type a message..."
          disabled={sending}
          rows={1}
          style={{ flex: 1 }}
        />

        <button
          onClick={onSend}
          disabled={sending || (!input.trim() && selectedImages.length === 0)}
          style={{ background: '#646cff', color: '#fff', border: 'none' }}
        >
          {isMobile ? (
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <line x1="22" y1="2" x2="11" y2="13" />
              <polygon points="22 2 15 22 11 13 2 9 22 2" />
            </svg>
          ) : (
            'Send'
          )}
        </button>
      </div>
    </div>
  )
}
