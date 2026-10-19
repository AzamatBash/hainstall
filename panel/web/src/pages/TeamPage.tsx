import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { api, translateError } from '../api'

const MSK = 'Europe/Moscow'
const WEEKDAYS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']

interface Member {
  id: string
  name: string
  color: string
}

interface Mark {
  member_id: string
  day: string
  status: 'off'
  note: string
}

function mskToday(): Date {
  const ymd = new Date().toLocaleDateString('en-CA', { timeZone: MSK })
  return new Date(`${ymd}T00:00:00Z`)
}

function addDays(d: Date, n: number): Date {
  const next = new Date(d)
  next.setUTCDate(next.getUTCDate() + n)
  return next
}

function ymd(d: Date): string {
  return d.toISOString().slice(0, 10)
}

function monthWeeks(year: number, month: number): Date[][] {
  const first = new Date(Date.UTC(year, month, 1))
  const weekday = first.getUTCDay()
  const mondayOffset = weekday === 0 ? 6 : weekday - 1
  const start = addDays(first, -mondayOffset)
  const weeks: Date[][] = []
  for (let w = 0; w < 6; w++) {
    const row: Date[] = []
    for (let d = 0; d < 7; d++) row.push(addDays(start, w * 7 + d))
    if (row.every((day) => day.getUTCMonth() !== month && day > first)) break
    weeks.push(row)
  }
  return weeks
}

function monthTitle(year: number, month: number): string {
  const label = new Date(Date.UTC(year, month, 1)).toLocaleDateString('ru-RU', {
    timeZone: 'UTC',
    month: 'long',
    year: 'numeric',
  })
  return label.replace(' г.', '')
}

function dayTitle(day: string): string {
  const d = new Date(`${day}T00:00:00Z`)
  return d.toLocaleDateString('ru-RU', {
    timeZone: 'UTC',
    weekday: 'long',
    day: 'numeric',
    month: 'long',
  })
}

export default function TeamPage() {
  const today = mskToday()
  const [members, setMembers] = useState<Member[]>([])
  const [marks, setMarks] = useState<Mark[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [cursor, setCursor] = useState({ year: today.getUTCFullYear(), month: today.getUTCMonth() })
  const [selectedId, setSelectedId] = useState('')
  const [renaming, setRenaming] = useState<{ id: string; name: string } | null>(null)

  const load = useCallback(async () => {
    setError('')
    try {
      const res = await api<{ members?: Member[]; marks?: Mark[] }>('/api/team')
      const list = Array.isArray(res.members) ? res.members : []
      setMembers(list)
      setMarks(Array.isArray(res.marks) ? res.marks : [])
      setSelectedId((cur) => (cur && list.some((m) => m.id === cur) ? cur : list[0]?.id || ''))
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const weeks = useMemo(() => monthWeeks(cursor.year, cursor.month), [cursor])
  const todayKey = ymd(today)
  const selected = members.find((m) => m.id === selectedId) || null

  const offByDay = useMemo(() => {
    const map = new Map<string, Member[]>()
    const byId = new Map(members.map((m) => [m.id, m]))
    for (const mark of marks) {
      if (mark.status !== 'off') continue
      const member = byId.get(mark.member_id)
      if (!member) continue
      const list = map.get(mark.day) || []
      list.push(member)
      map.set(mark.day, list)
    }
    return map
  }, [marks, members])

  const selectedDays = useMemo(() => {
    let n = 0
    for (const mark of marks) {
      if (mark.member_id !== selectedId || mark.status !== 'off') continue
      const d = new Date(`${mark.day}T00:00:00Z`)
      if (d.getUTCFullYear() === cursor.year && d.getUTCMonth() === cursor.month) n += 1
    }
    return n
  }, [marks, selectedId, cursor])

  async function addMember(e: FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    setBusy(true)
    setError('')
    try {
      const res = await api<{ member: Member }>('/api/team/members', {
        method: 'POST',
        body: JSON.stringify({ name: trimmed }),
      })
      setMembers((prev) => [...prev, res.member])
      setSelectedId(res.member.id)
      setName('')
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setBusy(false)
    }
  }

  async function saveName(id: string, nextName: string) {
    const trimmed = nextName.trim()
    setRenaming(null)
    if (!trimmed) return
    const prev = members.find((m) => m.id === id)
    if (!prev || prev.name === trimmed) return
    setMembers((list) => list.map((m) => (m.id === id ? { ...m, name: trimmed } : m)))
    try {
      await api(`/api/team/members/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body: JSON.stringify({ name: trimmed }),
      })
    } catch (err) {
      setMembers((list) => list.map((m) => (m.id === id ? prev : m)))
      setError(err instanceof Error ? err.message : translateError(String(err)))
    }
  }

  async function removeMember(id: string) {
    const member = members.find((m) => m.id === id)
    if (!member) return
    if (!window.confirm(`Убрать ${member.name} из графика? Отметки тоже удалятся.`)) return
    setError('')
    try {
      await api(`/api/team/members/${encodeURIComponent(id)}`, { method: 'DELETE' })
      const next = members.filter((m) => m.id !== id)
      setMembers(next)
      setMarks((list) => list.filter((m) => m.member_id !== id))
      setSelectedId((cur) => (cur === id ? next[0]?.id || '' : cur))
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    }
  }

  async function toggleDay(memberId: string, day: string) {
    const exists = marks.some((m) => m.member_id === memberId && m.day === day && m.status === 'off')
    const prev = marks
    setMarks((list) => {
      const rest = list.filter((m) => !(m.member_id === memberId && m.day === day))
      if (exists) return rest
      return [...rest, { member_id: memberId, day, status: 'off', note: '' }]
    })
    try {
      await api('/api/team/days', {
        method: 'PUT',
        body: JSON.stringify({ member_id: memberId, day, status: exists ? '' : 'off', note: '' }),
      })
    } catch (err) {
      setMarks(prev)
      setError(err instanceof Error ? err.message : translateError(String(err)))
    }
  }

  function shiftMonth(delta: number) {
    setCursor((cur) => {
      const d = new Date(Date.UTC(cur.year, cur.month + delta, 1))
      return { year: d.getUTCFullYear(), month: d.getUTCMonth() }
    })
  }

  return (
    <div className="shell">
      <header className="page-toolbar">
        <div className="page-toolbar-title">
          <h1>Команда</h1>
          <p className="muted">
            Выберите человека и нажимайте дни, когда он отдыхает. Один клик — один день.
          </p>
        </div>
        <form className="page-toolbar-actions row" onSubmit={(e) => void addMember(e)}>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Имя"
            maxLength={40}
            aria-label="Имя"
          />
          <button className="btn btn-primary" type="submit" disabled={busy || !name.trim()}>
            Добавить
          </button>
        </form>
      </header>

      {error && <p className="error">{error}</p>}
      {loading && <p className="muted">Загрузка…</p>}

      {!loading && members.length === 0 && (
        <p className="muted">Добавьте людей — появится календарь на месяц.</p>
      )}

      {!loading && members.length > 0 && (
        <div className="cal">
          <div className="cal-people" role="listbox" aria-label="Кого отмечаете">
            {members.map((member) => {
              const active = member.id === selectedId
              const count = marks.filter((m) => m.member_id === member.id && m.status === 'off').length
              return (
                <div key={member.id} className={`cal-person${active ? ' active' : ''}`}>
                  <button
                    type="button"
                    className="cal-person-pick"
                    aria-pressed={active}
                    onClick={() => setSelectedId(member.id)}
                  >
                    <span className="team-dot" style={{ background: member.color }} />
                    {renaming?.id === member.id ? (
                      <input
                        className="team-rename"
                        value={renaming.name}
                        autoFocus
                        maxLength={40}
                        onClick={(e) => e.stopPropagation()}
                        onChange={(e) => setRenaming({ id: member.id, name: e.target.value })}
                        onBlur={() => void saveName(member.id, renaming.name)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
                          if (e.key === 'Escape') setRenaming(null)
                        }}
                      />
                    ) : (
                      <span
                        className="team-name"
                        onDoubleClick={(e) => {
                          e.stopPropagation()
                          setRenaming({ id: member.id, name: member.name })
                        }}
                      >
                        {member.name}
                      </span>
                    )}
                    <span className="muted">{count}</span>
                  </button>
                  <button
                    className="btn btn-sm btn-ghost team-remove"
                    type="button"
                    aria-label={`Убрать ${member.name}`}
                    onClick={() => void removeMember(member.id)}
                  >
                    ×
                  </button>
                </div>
              )
            })}
          </div>

          <div className="cal-board">
            <div className="cal-nav">
              <button className="btn btn-sm" type="button" onClick={() => shiftMonth(-1)} aria-label="Предыдущий месяц">
                ‹
              </button>
              <div className="cal-month">{monthTitle(cursor.year, cursor.month)}</div>
              <button className="btn btn-sm" type="button" onClick={() => shiftMonth(1)} aria-label="Следующий месяц">
                ›
              </button>
              <button
                className="btn btn-sm"
                type="button"
                onClick={() => setCursor({ year: today.getUTCFullYear(), month: today.getUTCMonth() })}
              >
                Сегодня
              </button>
              {selected && (
                <span className="muted cal-hint">
                  Отмечаете: {selected.name}
                  {selectedDays > 0 ? ` · ${selectedDays} в этом месяце` : ''}
                </span>
              )}
            </div>

            <div className="cal-weekdays">
              {WEEKDAYS.map((label, i) => (
                <div key={label} className={i >= 5 ? 'is-weekend' : ''}>
                  {label}
                </div>
              ))}
            </div>

            <div className="cal-grid">
              {weeks.flat().map((day) => {
                const key = ymd(day)
                const inMonth = day.getUTCMonth() === cursor.month
                const weekend = day.getUTCDay() === 0 || day.getUTCDay() === 6
                const people = offByDay.get(key) || []
                const mine = selectedId && people.some((p) => p.id === selectedId)
                return (
                  <button
                    key={key}
                    type="button"
                    className={`cal-day${inMonth ? '' : ' outside'}${weekend ? ' weekend' : ''}${key === todayKey ? ' today' : ''}${mine ? ' mine' : ''}`}
                    disabled={!selected}
                    aria-label={dayTitle(key)}
                    aria-pressed={!!mine}
                    onClick={() => selected && void toggleDay(selected.id, key)}
                  >
                    <span className="cal-num">{day.getUTCDate()}</span>
                    <span className="cal-pills">
                      {people.map((person) => (
                        <span
                          key={person.id}
                          className="cal-pill"
                          style={{ background: person.color }}
                          title={`${person.name} отдыхает`}
                        >
                          {person.name}
                        </span>
                      ))}
                    </span>
                  </button>
                )
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
