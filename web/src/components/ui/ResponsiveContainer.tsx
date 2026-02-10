import React from 'react'

interface ResponsiveContainerProps {
  children: React.ReactNode
  className?: string
  style?: React.CSSProperties
}

export default function ResponsiveContainer({ children, className = '', style }: ResponsiveContainerProps) {
  return (
    <div 
      className={`responsive-container ${className}`} 
      style={{
        width: '100%',
        maxWidth: '1200px',
        margin: '0 auto',
        padding: '0 1.5rem',
        ...style
      }}
    >
      {children}
    </div>
  )
}
