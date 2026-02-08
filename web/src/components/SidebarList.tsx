import React from 'react'
import { NavLink } from 'react-router-dom'

interface SidebarListProps {
  title: string
  loading?: boolean
  error?: string
  onReload?: () => void
  children: React.ReactNode
}

export function SidebarList({ title, loading, error, onReload, children }: SidebarListProps) {
  return (
    <div className="persona-list-col">
      <div className="persona-list-header">
        <h2>{title}</h2>
        {onReload && (
          <button onClick={onReload} className="btn-icon" title="Reload">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M23 4v6h-6" /><path d="M1 20v-6h6" />
              <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
            </svg>
          </button>
        )}
      </div>
      <div className="persona-list">
        {loading && <p style={{ padding: '1rem', fontSize: '0.85rem', color: '#666' }}>Loading...</p>}
        {error && <p style={{ padding: '1rem', fontSize: '0.85rem' }} className="error">{error}</p>}
        {!loading && !error && children}
      </div>
    </div>
  )
}

interface SidebarItemProps {
  to?: string
  onClick?: () => void
  active?: boolean
  title: string | React.ReactNode
  subtitle?: string
  description?: string
  children?: React.ReactNode
}

export function SidebarItem({ to, onClick, active, title, subtitle, description, children }: SidebarItemProps) {
  const content = (
    <>
      <div className="persona-item-header">
        <span className="persona-name">{title}</span>
        {subtitle && <span className="persona-version">{subtitle}</span>}
        {children}
      </div>
      {description && <div className="persona-item-desc">{description}</div>}
    </>
  )

  if (to) {
    return (
      <NavLink
        to={to}
        className={({ isActive }) => `persona-list-item ${isActive || active ? 'active' : ''}`}
        onClick={onClick}
      >
        {content}
      </NavLink>
    )
  }

  return (
    <div
      className={`persona-list-item ${active ? 'active' : ''}`}
      onClick={onClick}
      style={{ cursor: 'pointer' }}
    >
      {content}
    </div>
  )
}
