import React, { useState, useRef, useEffect } from 'react'

export interface SelectOption {
  value: string
  label: string
}

interface SelectProps {
  options: SelectOption[]
  value: string
  onChange: (value: string) => void
  placeholder?: string
  disabled?: boolean
  className?: string
}

export default function Select({
  options,
  value,
  onChange,
  placeholder = 'Select an option',
  disabled = false,
  className = '',
}: SelectProps) {
  const [isOpen, setIsOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  const selectedOption = options.find((opt) => opt.value === value)

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  const handleSelect = (optionValue: string) => {
    onChange(optionValue)
    setIsOpen(false)
  }

  return (
    <div
      ref={containerRef}
      className={`custom-select-container ${className} ${disabled ? 'disabled' : ''}`}
      style={{ position: 'relative', width: '100%' }}
    >
      <div
        className={`custom-select-trigger ${isOpen ? 'open' : ''}`}
        onClick={() => !disabled && setIsOpen(!isOpen)}
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          padding: '0.55rem 0.85rem',
          background: '#0f0f1a',
          color: selectedOption ? '#e0e0e0' : '#7a7a9a',
          border: '1px solid rgba(255, 255, 255, 0.08)',
          borderRadius: '8px',
          fontSize: '0.9rem',
          cursor: disabled ? 'not-allowed' : 'pointer',
          transition: 'border-color 0.15s',
          borderColor: isOpen ? 'rgba(100, 108, 255, 0.4)' : 'rgba(255, 255, 255, 0.08)',
          opacity: disabled ? 0.6 : 1,
        }}
      >
        <span>{selectedOption ? selectedOption.label : placeholder}</span>
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          style={{
            transform: isOpen ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.2s',
            opacity: 0.5,
          }}
        >
          <polyline points="6 9 12 15 18 9"></polyline>
        </svg>
      </div>

      {isOpen && !disabled && (
        <div
          className="custom-select-options"
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            left: 0,
            right: 0,
            background: '#16162a',
            border: '1px solid rgba(255, 255, 255, 0.08)',
            borderRadius: '8px',
            maxHeight: '200px',
            overflowY: 'auto',
            zIndex: 50,
            boxShadow: '0 4px 20px rgba(0, 0, 0, 0.4)',
            padding: '4px',
          }}
        >
          {options.length === 0 ? (
            <div style={{ padding: '0.75rem', color: '#666', fontSize: '0.85rem', textAlign: 'center' }}>
              No options
            </div>
          ) : (
            options.map((option) => (
              <div
                key={option.value}
                className={`custom-select-option ${option.value === value ? 'selected' : ''}`}
                onClick={() => handleSelect(option.value)}
                style={{
                  padding: '0.5rem 0.75rem',
                  fontSize: '0.9rem',
                  color: option.value === value ? '#fff' : '#c0c0e0',
                  borderRadius: '4px',
                  cursor: 'pointer',
                  background: option.value === value ? 'rgba(100, 108, 255, 0.15)' : 'transparent',
                  transition: 'background 0.15s, color 0.15s',
                }}
                onMouseEnter={(e) => {
                  if (option.value !== value) {
                    e.currentTarget.style.background = 'rgba(255, 255, 255, 0.04)'
                    e.currentTarget.style.color = '#fff'
                  }
                }}
                onMouseLeave={(e) => {
                  if (option.value !== value) {
                    e.currentTarget.style.background = 'transparent'
                    e.currentTarget.style.color = '#c0c0e0'
                  }
                }}
              >
                {option.label}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}
