import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { getPersona } from '../api'
import type { PersonaDetail as PersonaDetailType } from '../types'

export default function PersonaDetail() {
  const { id } = useParams<{ id: string }>()
  const [persona, setPersona] = useState<PersonaDetailType | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!id) return
    setLoading(true)
    getPersona(id)
      .then(setPersona)
      .catch((e) => setError(e instanceof Error ? e.message : 'Load failed'))
      .finally(() => setLoading(false))
  }, [id])

  if (loading) return <p>Loading...</p>
  if (error) return <p className="error">Error: {error}</p>
  if (!persona) return <p>Not found.</p>

  const { meta, generation, anchors, variables, lore } = persona

  return (
    <div>
      <Link to="/">&larr; Back</Link>

      <h2>{meta.name}</h2>
      <p className="meta">{meta.id} &middot; v{meta.version} &middot; by {meta.author}</p>
      {meta.description && <p>{meta.description}</p>}

      <section>
        <h3>Generation Config</h3>
        <table>
          <tbody>
            <tr><td>Temperature</td><td>{generation.temperature}</td></tr>
            <tr><td>Max Tokens</td><td>{generation.max_tokens}</td></tr>
            <tr><td>Top P</td><td>{generation.top_p}</td></tr>
            <tr><td>Frequency Penalty</td><td>{generation.frequency_penalty}</td></tr>
            <tr><td>Presence Penalty</td><td>{generation.presence_penalty}</td></tr>
          </tbody>
        </table>
      </section>

      {variables && Object.keys(variables).length > 0 && (
        <section>
          <h3>Variables</h3>
          <table>
            <tbody>
              {Object.entries(variables).map(([k, v]) => (
                <tr key={k}><td>{k}</td><td>{String(v)}</td></tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      <section>
        <h3>Anchors ({anchors?.length ?? 0})</h3>
        {anchors?.map((a) => (
          <details key={a.id} className="anchor-item">
            <summary>
              <strong>{a.id}</strong>
              {a.tags?.map((t) => <span key={t} className="tag">{t}</span>)}
            </summary>
            <div className="anchor-content">
              <div>
                <h4>User</h4>
                <pre>{a.user_input}</pre>
              </div>
              <div>
                <h4>Bot</h4>
                <pre>{a.bot_response}</pre>
              </div>
            </div>
          </details>
        ))}
      </section>

      <section>
        <h3>Lore</h3>
        <pre className="lore-block">{lore}</pre>
      </section>
    </div>
  )
}
