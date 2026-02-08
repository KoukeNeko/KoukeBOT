import React from 'react'

interface CheckboxProps {
  checked: boolean
  onChange: (checked: boolean) => void
  label?: string
  disabled?: boolean
  className?: string
}

export default function Checkbox({
  checked,
  onChange,
  label,
  disabled = false,
  className = '',
}: CheckboxProps) {
  return (
    <label
      className={`custom-checkbox ${disabled ? 'disabled' : ''} ${className}`}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.6rem',
        cursor: disabled ? 'not-allowed' : 'pointer',
        userSelect: 'none',
      }}
    >
      <div
        className={`checkbox-box ${checked ? 'checked' : ''}`}
        onClick={(e) => {
          if (disabled) return
          // Prevent triggering label click twice if using label tag wrapping
          // But here we rely on the parent label tag behavior or manual
          // Actually, let's use a hidden input for accessibility and behavior
        }}
        style={{
          width: '18px',
          height: '18px',
          borderRadius: '4px',
          border: '1px solid',
          borderColor: checked ? '#646cff' : 'rgba(255, 255, 255, 0.3)',
          background: checked ? '#646cff' : 'transparent',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          transition: 'all 0.15s',
          flexShrink: 0,
        }}
      >
        <input
          type="checkbox"
          checked={checked}
          onChange={(e) => !disabled && onChange(e.target.checked)}
          disabled={disabled}
          style={{ display: 'none' }}
        />
        {checked && (
          <svg
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="white"
            strokeWidth="4"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <polyline points="20 6 9 17 4 12" />
          </svg>
        )}
      </div>
      {label && <span style={{ fontSize: '0.9rem', color: disabled ? '#666' : '#e0e0e0' }}>{label}</span>}
    </label>
  )
}
