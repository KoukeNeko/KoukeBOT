import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listPersonas, reloadPersonas } from '../api'
import type { PersonaSummary } from '../types'

export default function PersonaList() {
  const [personas, setPersonas] = useState<PersonaSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setPersonas(await listPersonas())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const handleReload = async () => {
    try {
      await reloadPersonas()
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Reload failed')
    }
  }

  if (loading) return <p>Loading personas...</p>
  if (error) return <p className="error">Error: {error}</p>

  return (
    <div>
      <div className="page-header">
        <h2>Personas</h2>
        <button onClick={handleReload}>Reload All</button>
      </div>

      {personas.length === 0 ? (
        <p>No personas loaded.</p>
      ) : (
        <div className="persona-grid">
          {personas.map((p) => (
            <Link key={p.id} to={`/personas/${p.id}`} className="persona-card">
              <h3>{p.name}</h3>
              <p className="meta">{p.id} &middot; v{p.version}</p>
              {p.description && <p>{p.description}</p>}
              <div className="tags">
                {p.anchor_tags?.map((tag) => (
                  <span key={tag} className="tag">{tag}</span>
                ))}
              </div>
              <p className="meta">{p.anchor_count} anchors &middot; by {p.author}</p>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
