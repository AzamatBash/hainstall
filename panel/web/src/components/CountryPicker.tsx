import { useEffect, useMemo, useRef, useState } from 'react'
import { COUNTRIES, flagEmoji } from '../countries'

type Props = {
  value: string
  onChange: (code: string) => void
  disabled?: boolean
}

export default function CountryPicker({ value, onChange, disabled }: Props) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const wrapRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    if (!needle) return COUNTRIES
    return COUNTRIES.filter(
      (c) =>
        c.name.toLowerCase().includes(needle) ||
        c.code.toLowerCase().includes(needle),
    )
  }, [q])

  useEffect(() => {
    if (!open) return
    function onDoc(e: MouseEvent) {
      if (!wrapRef.current?.contains(e.target as Node)) {
        setOpen(false)
        setQ('')
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  useEffect(() => {
    if (open) {
      window.setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  const flag = value ? flagEmoji(value) : ''

  return (
    <div className="country-picker" ref={wrapRef}>
      <button
        type="button"
        className={`country-trigger${value ? '' : ' empty'}`}
        disabled={disabled}
        title={value ? value : 'Assign country'}
        aria-label="Country"
        onClick={(e) => {
          e.stopPropagation()
          setOpen((o) => !o)
        }}
      >
        {flag || '🌐'}
      </button>
      {open && (
        <div className="country-pop" onClick={(e) => e.stopPropagation()}>
          <input
            ref={inputRef}
            className="country-search"
            placeholder="Search country…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                setOpen(false)
                setQ('')
              }
            }}
          />
          <button
            type="button"
            className="country-option clear"
            onClick={() => {
              onChange('')
              setOpen(false)
              setQ('')
            }}
          >
            No country
          </button>
          <ul className="country-list">
            {filtered.map((c) => (
              <li key={c.code}>
                <button
                  type="button"
                  className={`country-option${c.code === value ? ' active' : ''}`}
                  onClick={() => {
                    onChange(c.code)
                    setOpen(false)
                    setQ('')
                  }}
                >
                  <span className="country-flag">{flagEmoji(c.code)}</span>
                  <span className="country-name">{c.name}</span>
                  <span className="country-code mono">{c.code}</span>
                </button>
              </li>
            ))}
            {filtered.length === 0 && (
              <li className="muted country-empty">No matches</li>
            )}
          </ul>
        </div>
      )}
    </div>
  )
}
