import { useEffect, useState } from 'react'
import { Outlet, useParams } from 'react-router-dom'
import { listPersonas, reloadPersonas } from '../api'
import type { PersonaSummary } from '../types'
import { SidebarList, SidebarItem } from '../components/SidebarList'
import { useIsMobile } from '../hooks/useMediaQuery'

export default function Personas() {
  const [personas, setPersonas] = useState<PersonaSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const isMobile = useIsMobile()
  const { id } = useParams()
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

  // On mobile, if we have an ID, we only show the detail (Outlet)
  if (isMobile && id) {
    return (
      <div className="persona-layout mobile-detail">
        <div className="persona-detail-col">
          <Outlet />
        </div>
      </div>
    )
  }

  return (
    <div className="persona-layout">
      {(!isMobile || !id) && (
        <SidebarList 
          title="Personas" 
          loading={loading} 
          error={error} 
          onReload={handleReload}
        >
          {personas.map((p) => (
            <SidebarItem
              key={p.id}
              to={`/personas/${p.id}`}
              title={p.name}
              subtitle={`v${p.version}`}
              description={p.description}
            />
          ))}
        </SidebarList>
      )}
      {!isMobile && (
        <div className="persona-detail-col">
          <Outlet />
        </div>
      )}
    </div>
  )
}
