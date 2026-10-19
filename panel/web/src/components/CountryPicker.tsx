import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { COUNTRIES, flagEmoji } from '../countries'

type Props = {
  value: string
  onChange: (code: string) => void
  disabled?: boolean
}

type PopPos = {
  left: number
  top?: number
  bottom?: number
  maxHeight: number
}

const POP_WIDTH = 288 // ~18rem
const POP_GAP = 6
const POP_MAX = 288 // ~18rem

function computePopPos(anchor: DOMRect): PopPos {
  const width = Math.min(POP_WIDTH, window.innerWidth * 0.72)
  const spaceBelow = window.innerHeight - anchor.bottom - POP_GAP
  const spaceAbove = anchor.top - POP_GAP
  const preferUp = spaceBelow < 220 && spaceAbove > spaceBelow
  const available = preferUp ? spaceAbove : spaceBelow
  const maxHeight = Math.max(140, Math.min(POP_MAX, available - 4))

  let left = anchor.left
  if (left + width > window.innerWidth - 8) left = window.innerWidth - width - 8
  if (left < 8) left = 8

  if (preferUp) {
    return {
      left,
      bottom: window.innerHeight - anchor.top + POP_GAP,
      maxHeight,
    }
  }
  return {
    left,
    top: anchor.bottom + POP_GAP,
    maxHeight,
  }
}

export default function CountryPicker({ value, onChange, disabled }: Props) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [pos, setPos] = useState<PopPos | null>(null)
  const wrapRef = useRef<HTMLDivElement | null>(null)
  const popRef = useRef<HTMLDivElement | null>(null)
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

  const updatePos = () => {
    const el = wrapRef.current
    if (!el) return
    setPos(computePopPos(el.getBoundingClientRect()))
  }

  useLayoutEffect(() => {
    if (!open) {
      setPos(null)
      return
    }
    updatePos()
  }, [open])

  useEffect(() => {
    if (!open) return
    function onDoc(e: MouseEvent) {
      const t = e.target as Node
      if (wrapRef.current?.contains(t) || popRef.current?.contains(t)) return
      setOpen(false)
      setQ('')
    }
    function onReposition() {
      updatePos()
    }
    document.addEventListener('mousedown', onDoc)
    window.addEventListener('resize', onReposition)
    window.addEventListener('scroll', onReposition, true)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      window.removeEventListener('resize', onReposition)
      window.removeEventListener('scroll', onReposition, true)
    }
  }, [open])

  useEffect(() => {
    if (open) {
      window.setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  const flag = value ? flagEmoji(value) : ''

  const pop =
    open && pos
      ? createPortal(
          <div
            ref={popRef}
            className="country-pop country-pop-fixed"
            style={{
              left: pos.left,
              top: pos.top,
              bottom: pos.bottom,
              maxHeight: pos.maxHeight,
              width: Math.min(POP_WIDTH, window.innerWidth * 0.72),
            }}
            onClick={(e) => e.stopPropagation()}
          >
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
          </div>,
          document.body,
        )
      : null

  return (
    <div className="country-picker" ref={wrapRef}>
      <button
        type="button"
        className={`country-trigger${value ? '' : ' empty'}`}
        disabled={disabled}
        title={value ? value : 'Assign country'}
        aria-label="Country"
        aria-expanded={open}
        onClick={(e) => {
          e.stopPropagation()
          setOpen((o) => !o)
        }}
      >
        {flag || '🌐'}
      </button>
      {pop}
    </div>
  )
}
