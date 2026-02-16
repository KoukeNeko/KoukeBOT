import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import EditorModal from '../components/EditorModal'
import { getPersona, updatePersona } from '../api'
import { useIsMobile } from '../hooks/useMediaQuery'
import type {
  Anchor,
  GenerationConfig,
  PersonaCoreConfig,
  PersonaDetail as PersonaDetailType,
  PersonaMeta,
} from '../types'
import {
  applyCoreConfigToYAML,
  DEFAULT_PERSONA_CORE_CONFIG,
  parseConfigYaml,
} from '../features/persona/configYaml'

type EditingField =
  | { type: 'config_yaml' | 'lore' | 'system_template' | 'user_profile' }
  | { type: 'anchor_input' | 'anchor_response'; index: number }

export default function PersonaDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const isMobile = useIsMobile()

  const [persona, setPersona] = useState<PersonaDetailType | null>(null)
  const [coreConfig, setCoreConfig] = useState<PersonaCoreConfig | null>(null)
  const [yamlError, setYamlError] = useState<string | null>(null)
  const [lastEditedSource, setLastEditedSource] = useState<'form' | 'yaml' | null>(null)

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState<{ type: 'ok' | 'err'; text: string } | null>(null)
  const [dirty, setDirty] = useState(false)
  const [editingField, setEditingField] = useState<EditingField | null>(null)

  useEffect(() => {
    if (!id) return

    setLoading(true)
    setError('')
    setMessage(null)
    setDirty(false)
    setLastEditedSource(null)
    setYamlError(null)

    getPersona(id)
      .then((detail) => {
        setPersona(detail)
        const parsed = parseConfigYaml(detail.config_yaml)
        setCoreConfig(parsed.core)
        setYamlError(parsed.error)
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Load failed'))
      .finally(() => setLoading(false))
  }, [id])

  const markDirty = useCallback(() => {
    setDirty(true)
    setMessage(null)
  }, [])

  const applyCoreUpdate = useCallback(
    (updater: (current: PersonaCoreConfig) => PersonaCoreConfig) => {
      if (!persona || !coreConfig || yamlError) return

      const nextCore = updater(coreConfig)
      try {
        const nextConfigYaml = applyCoreConfigToYAML(persona.config_yaml, nextCore)
        setPersona({ ...persona, config_yaml: nextConfigYaml })
        setCoreConfig(nextCore)
        setYamlError(null)
        setLastEditedSource('form')
        markDirty()
      } catch (e) {
        setYamlError(e instanceof Error ? e.message : 'Invalid config.yaml')
      }
    },
    [coreConfig, markDirty, persona, yamlError],
  )

  const updateConfigYAML = useCallback(
    (value: string | undefined) => {
      if (!persona) return

      const nextConfigYaml = value ?? ''
      setPersona({ ...persona, config_yaml: nextConfigYaml })
      setLastEditedSource('yaml')
      markDirty()

      const parsed = parseConfigYaml(nextConfigYaml)
      setYamlError(parsed.error)
      if (parsed.core) {
        setCoreConfig(parsed.core)
      }
    },
    [markDirty, persona],
  )

  const updateMeta = (field: keyof PersonaMeta, value: string) => {
    applyCoreUpdate((current) => ({
      ...current,
      meta: {
        ...current.meta,
        [field]: value,
      },
    }))
  }

  const updateGeneration = (field: keyof GenerationConfig, value: string) => {
    const parsedNumber =
      field === 'max_tokens'
        ? Number.parseInt(value || '0', 10)
        : Number.parseFloat(value || '0')
    const safeNumber = Number.isFinite(parsedNumber) ? parsedNumber : 0

    applyCoreUpdate((current) => ({
      ...current,
      generation: {
        ...current.generation,
        [field]: field === 'max_tokens' ? Math.trunc(safeNumber) : safeNumber,
      },
    }))
  }

  const updateSystemTemplate = (value: string | undefined) => {
    applyCoreUpdate((current) => ({
      ...current,
      system_template: value ?? '',
    }))
  }

  const updateVariable = (key: string, raw: string) => {
    const num = Number(raw)
    const value: unknown = raw !== '' && !Number.isNaN(num) ? num : raw
    applyCoreUpdate((current) => ({
      ...current,
      variables: { ...current.variables, [key]: value },
    }))
  }

  const addVariable = () => {
    if (yamlError || !coreConfig) return
    const key = prompt('Variable name:')
    if (!key) return
    const trimmed = key.trim()
    if (!trimmed) return
    applyCoreUpdate((current) => ({
      ...current,
      variables: { ...current.variables, [trimmed]: '' },
    }))
  }

  const removeVariable = (key: string) => {
    applyCoreUpdate((current) => {
      const next = { ...current.variables }
      delete next[key]
      return { ...current, variables: next }
    })
  }

  const updateLore = (value: string | undefined) => {
    if (!persona) return
    setPersona({ ...persona, lore: value ?? '' })
    markDirty()
  }

  const updateUserProfile = (value: string | undefined) => {
    if (!persona) return
    setPersona({ ...persona, user_profile: value ?? '' })
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
    if (!id || !persona || yamlError) return

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

  const core = coreConfig ?? DEFAULT_PERSONA_CORE_CONFIG
  const { meta, generation, variables } = core
  const { anchors, lore, user_profile, config_yaml: configYaml } = persona

  const coreFormDisabled = Boolean(yamlError) || !coreConfig
  let modalTitle = ''
  let modalLanguage = 'plaintext'
  let modalValue = ''

  if (editingField) {
    switch (editingField.type) {
      case 'config_yaml':
        modalTitle = 'Edit config.yaml'
        modalLanguage = 'yaml'
        modalValue = configYaml
        break
      case 'lore':
        modalTitle = 'Edit Lore'
        modalLanguage = 'markdown'
        modalValue = lore
        break
      case 'user_profile':
        modalTitle = 'Edit User Profile (user.md)'
        modalLanguage = 'markdown'
        modalValue = user_profile
        break
      case 'system_template':
        modalTitle = 'Edit System Template'
        modalLanguage = 'yaml'
        modalValue = core.system_template
        break
      case 'anchor_input':
        modalTitle = `Edit Anchor Input (${anchors[editingField.index].id})`
        modalLanguage = 'plaintext'
        modalValue = anchors[editingField.index].user_input
        break
      case 'anchor_response':
        modalTitle = `Edit Anchor Response (${anchors[editingField.index].id})`
        modalLanguage = 'plaintext'
        modalValue = anchors[editingField.index].bot_response
        break
      default:
        break
    }
  }

  return (
    <div>
      <div className="page-header" style={{ justifyContent: isMobile ? 'space-between' : 'flex-end' }}>
        {isMobile && (
          <button className="btn-secondary" onClick={() => navigate('/personas')}>
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              style={{ marginRight: '4px', verticalAlign: 'text-bottom' }}
            >
              <path d="M19 12H5M12 19l-7-7 7-7" />
            </svg>
            Back
          </button>
        )}
        <button onClick={handleSave} disabled={saving || !dirty || Boolean(yamlError)}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>

      {message && <p className={message.type === 'ok' ? 'success' : 'error'}>{message.text}</p>}
      {yamlError && <p className="error">config.yaml invalid: {yamlError}</p>}

      {/* Config YAML */}
      <section>
        <h3>Config YAML</h3>
        <p className="hint" style={{ marginBottom: '0.75rem', color: '#666' }}>
          Core fields can be edited from the form below. Any custom keys should be edited in raw YAML.
        </p>
        <button className="btn-secondary" onClick={() => setEditingField({ type: 'config_yaml' })}>
          Edit config.yaml
        </button>
        {lastEditedSource && (
          <p className="meta" style={{ marginTop: '0.5rem' }}>
            Last config edit source: {lastEditedSource === 'form' ? 'Form' : 'YAML editor'}
          </p>
        )}
      </section>

      {/* Meta */}
      <section>
        <h3>Meta</h3>
        <label className="form-field">
          <span>ID</span>
          <input
            value={meta.id}
            onChange={(e) => updateMeta('id', e.target.value)}
            disabled={coreFormDisabled}
          />
        </label>
        <label className="form-field">
          <span>Name</span>
          <input
            value={meta.name}
            onChange={(e) => updateMeta('name', e.target.value)}
            disabled={coreFormDisabled}
          />
        </label>
        <label className="form-field">
          <span>Description</span>
          <input
            value={meta.description}
            onChange={(e) => updateMeta('description', e.target.value)}
            disabled={coreFormDisabled}
          />
        </label>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: isMobile ? '1fr' : '1fr 1fr',
            gap: isMobile ? '0' : '1rem',
          }}
        >
          <label className="form-field">
            <span>Version</span>
            <input
              value={meta.version}
              onChange={(e) => updateMeta('version', e.target.value)}
              disabled={coreFormDisabled}
            />
          </label>
          <label className="form-field">
            <span>Author</span>
            <input
              value={meta.author}
              onChange={(e) => updateMeta('author', e.target.value)}
              disabled={coreFormDisabled}
            />
          </label>
        </div>
      </section>

      {/* Generation Config */}
      <section>
        <h3>Generation Config</h3>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: isMobile ? '1fr' : '1fr 1fr',
            gap: isMobile ? '0' : '1rem',
          }}
        >
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
                disabled={coreFormDisabled}
              />
            </label>
          ))}
        </div>
      </section>

      {/* Variables */}
      <section>
        <h3>
          Variables
          <button
            onClick={addVariable}
            style={{ marginLeft: '0.75rem', fontSize: '0.8rem' }}
            disabled={coreFormDisabled}
          >
            + Add
          </button>
        </h3>
        {variables && Object.keys(variables).length > 0 ? (
          <div style={{ overflowX: 'auto' }}>
            <table>
              <tbody>
                {Object.entries(variables).map(([k, v]) => (
                  <tr key={k}>
                    <td style={{ width: isMobile ? '100px' : '180px' }}>{k}</td>
                    <td>
                      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                        <input
                          style={{
                            flex: 1,
                            background: '#0f0f1a',
                            border: '1px solid rgba(255,255,255,0.08)',
                            borderRadius: '6px',
                            padding: '0.35rem 0.6rem',
                            color: '#e0e0e0',
                            fontSize: '0.85rem',
                            minWidth: 0,
                          }}
                          value={String(v)}
                          onChange={(e) => updateVariable(k, e.target.value)}
                          disabled={coreFormDisabled}
                        />
                        <button
                          onClick={() => removeVariable(k)}
                          style={{ fontSize: '0.75rem', padding: '0.3rem 0.6rem', color: '#f44' }}
                          disabled={coreFormDisabled}
                        >
                          {isMobile ? '×' : 'Remove'}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
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
              {a.tags?.map((t) => (
                <span key={t} className="tag">
                  {t}
                </span>
              ))}
            </summary>
            <div style={{ marginTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: isMobile ? '1fr' : '1fr 1fr',
                  gap: isMobile ? '0.75rem' : '1rem',
                }}
              >
                <label className="form-field">
                  <span>ID</span>
                  <input value={a.id} onChange={(e) => updateAnchor(i, 'id', e.target.value)} />
                </label>
                <label className="form-field">
                  <span>Tags (comma-separated)</span>
                  <input
                    value={a.tags?.join(', ') ?? ''}
                    onChange={(e) =>
                      updateAnchor(
                        i,
                        'tags',
                        e.target.value
                          .split(',')
                          .map((t) => t.trim())
                          .filter(Boolean),
                      )
                    }
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
                    {a.user_input || (
                      <span style={{ fontStyle: 'italic', opacity: 0.5 }}>Empty (Click to edit)</span>
                    )}
                  </div>
                </div>
                <div>
                  <h4>Bot Response</h4>
                  <div
                    className="preview-box"
                    onClick={() => setEditingField({ type: 'anchor_response', index: i })}
                    title="Click to edit"
                  >
                    {a.bot_response || (
                      <span style={{ fontStyle: 'italic', opacity: 0.5 }}>Empty (Click to edit)</span>
                    )}
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
        <button
          className="btn-secondary"
          onClick={() => setEditingField({ type: 'system_template' })}
          disabled={coreFormDisabled}
        >
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

      {/* User Profile (Memory) */}
      <section>
        <h3>User Profile (Memory)</h3>
        <p className="hint" style={{ marginBottom: '0.75rem', color: '#666' }}>
          Persistent user-specific memory or profile data (user.md).
        </p>
        <button className="btn-secondary" onClick={() => setEditingField({ type: 'user_profile' })}>
          Edit User Profile
        </button>
      </section>

      {/* Editor Modal */}
      {editingField && (
        <EditorModal
          title={modalTitle}
          language={modalLanguage}
          value={modalValue}
          onChange={(v) => {
            if (editingField.type === 'config_yaml') updateConfigYAML(v)
            else if (editingField.type === 'lore') updateLore(v)
            else if (editingField.type === 'user_profile') updateUserProfile(v)
            else if (editingField.type === 'system_template') updateSystemTemplate(v)
            else if (editingField.type === 'anchor_input') updateAnchor(editingField.index, 'user_input', v ?? '')
            else if (editingField.type === 'anchor_response') updateAnchor(editingField.index, 'bot_response', v ?? '')
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
