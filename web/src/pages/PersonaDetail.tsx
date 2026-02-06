import { useEffect, useState, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { getPersona, updatePersona } from '../api'
import type { PersonaDetail as PersonaDetailType, Anchor } from '../types'
import EditorModal from '../components/EditorModal'

export default function PersonaDetail() {
  const { id } = useParams<{ id: string }>()
  const [persona, setPersona] = useState<PersonaDetailType | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState<{ type: 'ok' | 'err'; text: string } | null>(null)
  const [dirty, setDirty] = useState(false)

  // Track which field is open in the modal
  const [editingField, setEditingField] = useState<{
    type: 'lore' | 'system_template' | 'anchor_input' | 'anchor_response'
    index?: number
  } | null>(null)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    getPersona(id)
      .then(setPersona)
      .catch((e) => setError(e instanceof Error ? e.message : 'Load failed'))
      .finally(() => setLoading(false))
  }, [id])

  const markDirty = useCallback(() => {
    setDirty(true)
    setMessage(null)
  }, [])

  const updateMeta = (field: string, value: string) => {
    if (!persona) return
    setPersona({ ...persona, meta: { ...persona.meta, [field]: value } })
    markDirty()
  }

  const updateGeneration = (field: string, value: string) => {
    if (!persona) return
    const num = parseFloat(value)
    setPersona({
      ...persona,
      generation: { ...persona.generation, [field]: isNaN(num) ? 0 : num },
    })
    markDirty()
  }

  const updateLore = (value: string | undefined) => {
    if (!persona) return
    setPersona({ ...persona, lore: value ?? '' })
    markDirty()
  }

  const updateSystemTemplate = (value: string | undefined) => {
    if (!persona) return
    setPersona({ ...persona, system_template: value ?? '' })
    markDirty()
  }

  const updateVariable = (key: string, raw: string) => {
    if (!persona) return
    const num = Number(raw)
    const value: unknown = raw !== '' && !isNaN(num) ? num : raw
    setPersona({ ...persona, variables: { ...persona.variables, [key]: value } })
    markDirty()
  }

  const addVariable = () => {
    if (!persona) return
    const key = prompt('Variable name:')
    if (!key) return
    setPersona({ ...persona, variables: { ...persona.variables, [key]: '' } })
    markDirty()
  }

  const removeVariable = (key: string) => {
    if (!persona) return
    const next = { ...persona.variables }
    delete next[key]
    setPersona({ ...persona, variables: next })
    markDirty()
  }

  const updateAnchor = (index: number, field: keyof Anchor, value: string | string[]) => {
    if (!persona) return
    const anchors = [...persona.anchors]
    anchors[index] = { ...anchors[index], [field]: value }
    setPersona({ ...persona, anchors })
    markDirty()
  }

  const addAnchor = () => {
    if (!persona) return
    const newAnchor: Anchor = {
      id: `anchor-${Date.now()}`,
      tags: [],
      user_input: '',
      bot_response: '',
    }
    setPersona({ ...persona, anchors: [...(persona.anchors ?? []), newAnchor] })
    markDirty()
  }

  const removeAnchor = (index: number) => {
    if (!persona) return
    const anchors = persona.anchors.filter((_, i) => i !== index)
    setPersona({ ...persona, anchors })
    markDirty()
  }

  const handleSave = async () => {
    if (!id || !persona) return
    setSaving(true)
    setMessage(null)
    try {
      await updatePersona(id, persona)
      setMessage({ type: 'ok', text: 'Persona saved successfully.' })
      setDirty(false)
    } catch (e) {
      setMessage({ type: 'err', text: e instanceof Error ? e.message : 'Save failed' })
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <p>Loading...</p>
  if (error) return <p className="error">Error: {error}</p>
  if (!persona) return <p>Not found.</p>

  const { meta, generation, anchors, variables, lore, system_template } = persona

  return (
    <div>
      <div className="page-header" style={{ justifyContent: 'flex-end' }}>
        <button onClick={handleSave} disabled={saving || !dirty}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>

      {message && (
        <p className={message.type === 'ok' ? 'success' : 'error'}>{message.text}</p>
      )}

      {/* Meta */}
      <section>
        <h3>Meta</h3>
        <label className="form-field">
          <span>Name</span>
          <input value={meta.name} onChange={(e) => updateMeta('name', e.target.value)} />
        </label>
        <label className="form-field">
          <span>Description</span>
          <input value={meta.description} onChange={(e) => updateMeta('description', e.target.value)} />
        </label>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
          <label className="form-field">
            <span>Version</span>
            <input value={meta.version} onChange={(e) => updateMeta('version', e.target.value)} />
          </label>
          <label className="form-field">
            <span>Author</span>
            <input value={meta.author} onChange={(e) => updateMeta('author', e.target.value)} />
          </label>
        </div>
      </section>

      {/* Generation Config */}
      <section>
        <h3>Generation Config</h3>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
          {(
            [
              ['temperature', 'Temperature'],
              ['max_tokens', 'Max Tokens'],
              ['top_p', 'Top P'],
              ['frequency_penalty', 'Frequency Penalty'],
              ['presence_penalty', 'Presence Penalty'],
            ] as const
          ).map(([key, label]) => (
            <label className="form-field" key={key}>
              <span>{label}</span>
              <input
                type="number"
                step={key === 'max_tokens' ? 1 : 0.01}
                value={generation[key]}
                onChange={(e) => updateGeneration(key, e.target.value)}
              />
            </label>
          ))}
        </div>
      </section>

      {/* Variables */}
      <section>
        <h3>
          Variables
          <button onClick={addVariable} style={{ marginLeft: '0.75rem', fontSize: '0.8rem' }}>
            + Add
          </button>
        </h3>
        {variables && Object.keys(variables).length > 0 ? (
          <table>
            <tbody>
              {Object.entries(variables).map(([k, v]) => (
                <tr key={k}>
                  <td>{k}</td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                      <input
                        style={{ flex: 1, background: '#0f0f1a', border: '1px solid rgba(255,255,255,0.08)', borderRadius: '6px', padding: '0.35rem 0.6rem', color: '#e0e0e0', fontSize: '0.85rem' }}
                        value={String(v)}
                        onChange={(e) => updateVariable(k, e.target.value)}
                      />
                      <button onClick={() => removeVariable(k)} style={{ fontSize: '0.75rem', padding: '0.3rem 0.6rem', color: '#f44' }}>
                        Remove
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="meta">No variables defined.</p>
        )}
      </section>

      {/* Anchors */}
      <section>
        <h3>
          Anchors ({anchors?.length ?? 0})
          <button onClick={addAnchor} style={{ marginLeft: '0.75rem', fontSize: '0.8rem' }}>
            + Add
          </button>
        </h3>
        {anchors?.map((a, i) => (
          <details key={`${a.id}-${i}`} className="anchor-item">
            <summary>
              <strong>{a.id}</strong>
              {a.tags?.map((t) => <span key={t} className="tag">{t}</span>)}
            </summary>
            <div style={{ marginTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <label className="form-field">
                  <span>ID</span>
                  <input value={a.id} onChange={(e) => updateAnchor(i, 'id', e.target.value)} />
                </label>
                <label className="form-field">
                  <span>Tags (comma-separated)</span>
                  <input
                    value={a.tags?.join(', ') ?? ''}
                    onChange={(e) => updateAnchor(i, 'tags', e.target.value.split(',').map((t) => t.trim()).filter(Boolean))}
                  />
                </label>
              </div>
              <div className="anchor-content">
                <div>
                  <h4>User Input</h4>
                  <div
                    className="preview-box"
                    onClick={() => setEditingField({ type: 'anchor_input', index: i })}
                    title="Click to edit"
                  >
                    {a.user_input || <span style={{ fontStyle: 'italic', opacity: 0.5 }}>Empty (Click to edit)</span>}
                  </div>
                </div>
                <div>
                  <h4>Bot Response</h4>
                  <div
                    className="preview-box"
                    onClick={() => setEditingField({ type: 'anchor_response', index: i })}
                    title="Click to edit"
                  >
                    {a.bot_response || <span style={{ fontStyle: 'italic', opacity: 0.5 }}>Empty (Click to edit)</span>}
                  </div>
                </div>
              </div>
              <button onClick={() => removeAnchor(i)} style={{ alignSelf: 'flex-end', color: '#f44', fontSize: '0.8rem' }}>
                Remove Anchor
              </button>
            </div>
          </details>
        ))}
      </section>

      {/* System Template */}
      <section>
        <h3>System Template</h3>
        <p className="hint" style={{ marginBottom: '0.75rem', color: '#666' }}>
          Available placeholders: <code>{'{{.Lore}}'}</code>, <code>{'{{.Anchors}}'}</code>
        </p>
        <button className="btn-secondary" onClick={() => setEditingField({ type: 'system_template' })}>
          Edit System Template
        </button>
      </section>

      {/* Lore */}
      <section>
        <h3>Lore</h3>
        <button className="btn-secondary" onClick={() => setEditingField({ type: 'lore' })}>
          Edit Lore
        </button>
      </section>

      {/* Editor Modal */}
      {editingField && (
        <EditorModal
          title={
            editingField.type === 'lore'
              ? 'Edit Lore'
              : editingField.type === 'system_template'
              ? 'Edit System Template'
              : editingField.type === 'anchor_input'
              ? `Edit Anchor Input (${anchors[editingField.index!].id})`
              : `Edit Anchor Response (${anchors[editingField.index!].id})`
          }
          language={
            editingField.type === 'lore' ? 'markdown' : editingField.type === 'system_template' ? 'yaml' : 'plaintext'
          }
          value={
            editingField.type === 'lore'
              ? lore
              : editingField.type === 'system_template'
              ? system_template
              : editingField.type === 'anchor_input'
              ? anchors[editingField.index!].user_input
              : anchors[editingField.index!].bot_response
          }
          onChange={(v) => {
            if (editingField.type === 'lore') updateLore(v)
            else if (editingField.type === 'system_template') updateSystemTemplate(v)
            else if (editingField.type === 'anchor_input') updateAnchor(editingField.index!, 'user_input', v ?? '')
            else if (editingField.type === 'anchor_response') updateAnchor(editingField.index!, 'bot_response', v ?? '')
          }}
          onClose={() => setEditingField(null)}
          hint={
            editingField.type === 'system_template' ? (
              <span>
                Available placeholders: <code>{'{{.Lore}}'}</code>, <code>{'{{.Anchors}}'}</code>
              </span>
            ) : null
          }
        />
      )}
    </div>
  )
}