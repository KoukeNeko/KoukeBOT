import { useEffect, useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { listPersonas, reloadPersonas } from '../api'
import type { PersonaSummary } from '../types'

export default function Personas() {
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

  return (
    <div className="persona-layout">
      <div className="persona-list-col">
        <div className="persona-list-header">
          <h2>Personas</h2>
          <button onClick={handleReload} className="btn-icon" title="Reload">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M23 4v6h-6"/><path d="M1 20v-6h6"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>
          </button>
        </div>
        <div className="persona-list">
          {loading && <p className="p-4 text-sm text-gray">Loading...</p>}
          {error && <p className="p-4 text-sm error">{error}</p>}
          {!loading && !error && personas.map((p) => (
            <NavLink
              key={p.id}
              to={`/personas/${p.id}`}
              className={({ isActive }) => `persona-list-item ${isActive ? 'active' : ''}`}
            >
              <div className="persona-item-header">
                <span className="persona-name">{p.name}</span>
                <span className="persona-version">v{p.version}</span>
              </div>
              <div className="persona-item-desc">{p.description}</div>
            </NavLink>
          ))}
        </div>
      </div>
      <div className="persona-detail-col">
        <Outlet />
      </div>
    </div>
  )
}
