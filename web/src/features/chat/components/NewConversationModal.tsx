import type { PersonaSummary } from '../../../types'

type Props = {
  open: boolean
  personas: PersonaSummary[]
  selectedPersonaID: string
  onClose: () => void
  onSelectPersona: (personaId: string) => void
  onConfirm: () => void
}

export default function NewConversationModal({
  open,
  personas,
  selectedPersonaID,
  onClose,
  onSelectPersona,
  onConfirm,
}: Props) {
  if (!open) return null

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content" onClick={(event) => event.stopPropagation()} style={{ width: '450px', height: 'auto', maxHeight: '85vh' }}>
        <div className="modal-header">
          <h3>建立新對話</h3>
          <button onClick={onClose} className="close-btn">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        <div className="modal-body" style={{ padding: '1.25rem', overflowY: 'auto' }}>
          <p className="meta" style={{ marginBottom: '1.25rem' }}>
            請選擇一個 Persona 開始新的對話記錄。
          </p>

          <div className="persona-selection-list" style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            {personas.map((persona) => (
              <div
                key={persona.id}
                className={`persona-list-item ${selectedPersonaID === persona.id ? 'active' : ''}`}
                onClick={() => onSelectPersona(persona.id)}
                style={{ cursor: 'pointer', border: '1px solid rgba(255,255,255,0.06)', borderRadius: '10px' }}
              >
                <div className="persona-item-header">
                  <span className="persona-name">{persona.name}</span>
                  <span className="persona-version">v{persona.version}</span>
                </div>
                {persona.description && (
                  <div
                    className="persona-item-desc"
                    style={{ WebkitLineClamp: 2, display: '-webkit-box', WebkitBoxOrient: 'vertical', whiteSpace: 'normal' }}
                  >
                    {persona.description}
                  </div>
                )}
              </div>
            ))}
          </div>

          <div style={{ display: 'flex', gap: '0.75rem', marginTop: '1.75rem', justifyContent: 'flex-end' }}>
            <button onClick={onClose} className="btn-secondary">
              取消
            </button>
            <button
              onClick={onConfirm}
              disabled={!selectedPersonaID}
              style={{ background: '#646cff', color: '#fff', borderColor: '#646cff' }}
            >
              開始對話
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
