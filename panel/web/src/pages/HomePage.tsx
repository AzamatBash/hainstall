import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api, formatBitrateShort } from '../api'
import TrafficMarks from '../components/TrafficMarks'

type HomePayload = {
  panel: {
    pid: number
    uptime_sec: number
    goroutines: number
    heap_alloc: number
    heap_sys: number
    sys: number
    num_gc: number
    go_version: string
  }
  host: {
    hostname: string
    uptime_sec: number
    cpu_cores: number
    load1: number
    load5: number
    load15: number
    mem_total: number
    mem_used: number
    disk_total: number
    disk_used: number
  }
  alerts?: HomeAlert[]
  traffic?: HomeTraffic
}

type TrafficTrend = 'up' | 'down' | 'flat' | 'unknown'

type HomeTraffic = {
  panels: {
    id: string
    name: string
    trend: TrafficTrend
    delta: number
    tx_bps: number
    rx_bps: number
    days: { t: number; tx: number; rx: number }[]
  }[]
}

type HomeAlert = {
  panel_id: string
  panel_name: string
  remna_uuid: string
  name: string
  address: string
  drop: boolean
  decline: boolean
  drop_hint: string
  decline_hint: string
  note: string
}

function alertKey(a: HomeAlert) {
  return `${a.panel_id}:${a.remna_uuid}`
}

function groupAlerts(alerts: HomeAlert[]) {
  const groups: { id: string; name: string; items: HomeAlert[] }[] = []
  const index = new Map<string, number>()
  for (const alert of alerts) {
    const id = alert.panel_id || 'none'
    const at = index.get(id)
    if (at === undefined) {
      index.set(id, groups.length)
      groups.push({ id, name: alert.panel_name || 'Без панели', items: [alert] })
    } else {
      groups[at].items.push(alert)
    }
  }
  return groups
}

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']

function formatIec(raw: number): string {
  if (!Number.isFinite(raw) || raw < 0) return '—'
  let n = raw
  let i = 0
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024
    i++
  }
  const digits = i === 0 ? 0 : n >= 100 ? 0 : 2
  return `${n.toFixed(digits)} ${UNITS[i]}`
}

function formatUptime(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '—'
  const s = Math.floor(sec)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}д ${h}ч`
  if (h > 0) return `${h}ч ${m}м`
  if (m > 0) return `${m}м`
  return `${s}с`
}

function pct(used: number, total: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return 0
  return Math.max(0, Math.min(100, (used / total) * 100))
}

const TREND_LABEL: Record<TrafficTrend, string> = {
  up: 'Рост',
  down: 'Спад',
  flat: 'Держится',
  unknown: 'Мало данных',
}

function trendArrow(trend: TrafficTrend) {
  if (trend === 'up') return '↑'
  if (trend === 'down') return '↓'
  if (trend === 'flat') return '→'
  return '·'
}

function formatDelta(delta: number) {
  if (!Number.isFinite(delta) || delta === 0) return ''
  const pct = Math.round(delta * 100)
  if (pct === 0) return ''
  return pct > 0 ? `+${pct}%` : `${pct}%`
}

function sparkPoints(days: { t: number; tx: number; rx: number }[], pick: 'tx' | 'rx', w: number, h: number) {
  const minT = days[0].t
  const maxT = days[days.length - 1].t
  const maxB = Math.max(...days.map((d) => Math.max(d.tx, d.rx)), 1)
  return days
    .map((d) => {
      const x = maxT === minT ? w / 2 : ((d.t - minT) / (maxT - minT)) * (w - 6) + 3
      const y = h - 4 - ((pick === 'tx' ? d.tx : d.rx) / maxB) * (h - 10)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

function Spark({ days }: { days: { t: number; tx: number; rx: number }[] }) {
  if (days.length < 2) return null
  const w = 168
  const h = 42
  return (
    <svg className="home-spark" viewBox={`0 0 ${w} ${h}`} aria-hidden>
      <polyline className="rx" points={sparkPoints(days, 'rx', w, h)} />
      <polyline className="tx" points={sparkPoints(days, 'tx', w, h)} />
    </svg>
  )
}

function Icon({ d }: { d: string }) {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <path d={d} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rt-metric">
      <div className="rt-metric-label">{label}</div>
      <div className="rt-metric-value">{value}</div>
    </div>
  )
}

function Meter({
  label,
  used,
  total,
  tone,
}: {
  label: string
  used: number
  total: number
  tone: string
}) {
  const p = pct(used, total)
  return (
    <div className="rt-meter">
      <div className="rt-kicker">{label}</div>
      <div className="rt-bar" aria-hidden>
        <span className={tone} style={{ width: `${p}%` }} />
      </div>
      <div className="rt-meter-line">
        {formatIec(used)}
        <span> / {formatIec(total)}</span>
        <span> ({p.toFixed(0)}%)</span>
      </div>
    </div>
  )
}

function AlertNoteDialog({
  alert,
  value,
  dirty,
  saving,
  error,
  onChange,
  onSave,
  onClose,
}: {
  alert: HomeAlert
  value: string
  dirty: boolean
  saving: boolean
  error: string
  onChange: (value: string) => void
  onSave: () => void
  onClose: () => void
}) {
  const title = alert.name || alert.address || alert.remna_uuid
  const hints = [alert.drop ? alert.drop_hint : '', alert.decline ? alert.decline_hint : ''].filter(Boolean)
  const meta = [alert.panel_name, alert.address].filter(Boolean).join(' · ')
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal stack home-alert-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="home-alert-dialog-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="home-alert-dialog-head">
          <TrafficMarks
            drop={alert.drop}
            decline={alert.decline}
            dropHint={alert.drop_hint}
            declineHint={alert.decline_hint}
          />
          <div>
            <h2 id="home-alert-dialog-title">{title}</h2>
            {meta && <div className="home-alert-dialog-meta">{meta}</div>}
          </div>
        </div>
        {hints.map((line) => (
          <p className="home-alert-dialog-hint" key={line}>
            {line}
          </p>
        ))}
        <label className="home-alert-dialog-note">
          Заметка
          <textarea
            value={value}
            maxLength={500}
            placeholder="Почему так"
            aria-label={`Заметка: ${title}`}
            onChange={(e) => onChange(e.target.value)}
          />
        </label>
        {error && <p className="error">{error}</p>}
        <div className="modal-actions">
          <Link className="btn btn-ghost" to={`/stats?node=${encodeURIComponent(alertKey(alert))}`}>
            На статистике
          </Link>
          <button className="btn btn-ghost" type="button" onClick={onClose}>
            Закрыть
          </button>
          <button className="btn btn-primary" type="button" disabled={!dirty || saving} onClick={onSave}>
            {saving ? '…' : 'Сохранить'}
          </button>
        </div>
      </div>
    </div>
  )
}

export default function HomePage() {
  const [data, setData] = useState<HomePayload | null>(null)
  const [error, setError] = useState('')
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [savingKey, setSavingKey] = useState('')
  const [noteError, setNoteError] = useState('')
  const [openKey, setOpenKey] = useState('')

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const res = await api<HomePayload>('/api/home')
        if (!cancelled) {
          setData(res)
          setError('')
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Не удалось загрузить сводку')
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 5000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  useEffect(() => {
    if (!openKey) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpenKey('')
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [openKey])

  async function saveNote(alert: HomeAlert) {
    const key = alertKey(alert)
    const note = (drafts[key] ?? alert.note).trim()
    if (note === alert.note) {
      setDrafts((prev) => {
        if (!(key in prev)) return prev
        const next = { ...prev }
        delete next[key]
        return next
      })
      return
    }
    setSavingKey(key)
    setNoteError('')
    try {
      await api(`/api/home/alerts/${encodeURIComponent(alert.panel_id)}/${encodeURIComponent(alert.remna_uuid)}`, {
        method: 'PATCH',
        body: JSON.stringify({ note }),
      })
      setDrafts((prev) => {
        const next = { ...prev }
        delete next[key]
        return next
      })
      setData((prev) =>
        prev
          ? {
              ...prev,
              alerts: (prev.alerts ?? []).map((row) => (alertKey(row) === key ? { ...row, note } : row)),
            }
          : prev,
      )
    } catch (err) {
      setNoteError(err instanceof Error ? err.message : 'Не удалось сохранить заметку')
    } finally {
      setSavingKey('')
    }
  }

  const panel = data?.panel
  const host = data?.host
  const alerts = data?.alerts ?? []
  const alertGroups = groupAlerts(alerts)
  const openAlert = alerts.find((row) => alertKey(row) === openKey) ?? null
  const traffic = data?.traffic

  return (
    <div className="shell home-shell">
      <h2 className="home-section-title">Runtime</h2>
      {error && <p className="error">{error}</p>}
      {!data && !error && <p className="muted">Загрузка…</p>}

      {panel && host && (
        <div className="rt-grid">
          <article className="rt-card cyan">
            <header className="rt-head">
              <span className="rt-icon cyan" aria-hidden>
                <Icon d="M7 18h10a4 4 0 0 0 .4-8 5.5 5.5 0 0 0-10.6 1.6A3.5 3.5 0 0 0 7 18Z" />
              </span>
              <div className="rt-name">hapanel</div>
              <span className="rt-badge">PID · {panel.pid}</span>
            </header>
            <div className="rt-panel">
              <Meter label="Память" used={panel.heap_alloc} total={panel.heap_sys || panel.sys} tone="cyan" />
              <div className="rt-foot">
                <Metric label="Горутины" value={panel.goroutines.toLocaleString('ru-RU')} />
                <Metric label="Аптайм" value={formatUptime(panel.uptime_sec)} />
                <Metric label="У ОС" value={formatIec(panel.sys)} />
                <Metric label="Go" value={panel.go_version.replace(/^go/, '')} />
              </div>
            </div>
          </article>

          <article className="rt-card violet">
            <header className="rt-head">
              <span className="rt-icon violet" aria-hidden>
                <Icon d="M4 6h16v5H4V6Zm0 7h16v5H4v-5ZM7 8.5h.01M7 15.5h.01" />
              </span>
              <div className="rt-name">{host.hostname || 'сервер'}</div>
              <span className="rt-badge">{host.cpu_cores} ядер</span>
            </header>
            <div className="rt-panel">
              <Meter label="Память" used={host.mem_used} total={host.mem_total} tone="violet" />
              <Meter
                label="Диск"
                used={host.disk_used}
                total={host.disk_total}
                tone={pct(host.disk_used, host.disk_total) >= 80 ? 'amber' : 'green'}
              />
              <div className="rt-foot">
                <Metric label="Load 1м" value={host.load1.toFixed(2)} />
                <Metric label="5м" value={host.load5.toFixed(2)} />
                <Metric label="15м" value={host.load15.toFixed(2)} />
                <Metric label="Аптайм" value={formatUptime(host.uptime_sec)} />
              </div>
            </div>
          </article>
        </div>
      )}

      {traffic && (
        <section className="home-traffic">
          <h2 className="home-section-title">Трафик за 7 дней</h2>
          {(traffic.panels ?? []).length === 0 && <p className="muted">Нет трафика за эту неделю.</p>}
          <div className="home-traffic-grid">
            {(traffic.panels ?? []).map((item) => (
              <article className={`home-traffic-total ${item.trend}`} key={item.id}>
                <div className="home-traffic-kicker">{item.name}</div>
                <div className="home-traffic-verdict">
                  <span>{trendArrow(item.trend)}</span>
                  {TREND_LABEL[item.trend] || 'Мало данных'}
                  {item.trend !== 'unknown' && formatDelta(item.delta) && <em>{formatDelta(item.delta)}</em>}
                </div>
                <div className="home-traffic-rates">
                  <div>
                    <div className="home-traffic-rate-label">TX</div>
                    <div className="home-traffic-rate">{formatBitrateShort(item.tx_bps)}</div>
                  </div>
                  <div>
                    <div className="home-traffic-rate-label">RX</div>
                    <div className="home-traffic-rate rx">{formatBitrateShort(item.rx_bps)}</div>
                  </div>
                </div>
                <Spark days={item.days ?? []} />
              </article>
            ))}
          </div>
        </section>
      )}

      <section className="home-alerts">
        <h2 className="home-section-title">Предупреждения</h2>
        {data && alerts.length === 0 && <p className="muted">Сейчас предупреждений нет.</p>}
        {alertGroups.map((group) => (
          <div className="home-alert-panel" key={group.id}>
            <h3>{group.name}</h3>
            <div className="home-alert-grid">
              {group.items.map((alert) => {
                const key = alertKey(alert)
                const title = alert.name || alert.address || alert.remna_uuid
                const meta = [alert.panel_name, alert.address].filter(Boolean).join(' · ')
                const tone = `${alert.drop ? ' drop' : ''}${alert.decline ? ' decline' : ''}`
                return (
                  <button
                    type="button"
                    className={`home-alert${tone}${alert.note ? ' noted' : ''}`}
                    key={key}
                    onClick={() => {
                      setNoteError('')
                      setOpenKey(key)
                    }}
                  >
                    <span className="home-alert-body">
                      <span className="home-alert-top">
                        <TrafficMarks
                          drop={alert.drop}
                          decline={alert.decline}
                          dropHint={alert.drop_hint}
                          declineHint={alert.decline_hint}
                        />
                        <span className="home-alert-name">{title}</span>
                      </span>
                      {meta && <span className="home-alert-ip">{meta}</span>}
                    </span>
                    {alert.note && (
                      <span className="home-alert-note-mark" title="Есть заметка">
                        <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden>
                          <path
                            d="M3.6 2.4h5.4L12.6 6v7.2a.8.8 0 0 1-.8.8H3.6a.8.8 0 0 1-.8-.8V3.2a.8.8 0 0 1 .8-.8Z"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="1.3"
                            strokeLinejoin="round"
                          />
                          <path d="M8.8 2.5V6h3.5" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
                          <path d="M5 9h5.2M5 11.3h3.4" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
                        </svg>
                      </span>
                    )}
                  </button>
                )
              })}
            </div>
          </div>
        ))}
      </section>

      {openAlert && (
        <AlertNoteDialog
          alert={openAlert}
          value={drafts[alertKey(openAlert)] ?? openAlert.note}
          dirty={(drafts[alertKey(openAlert)] ?? openAlert.note).trim() !== openAlert.note}
          saving={savingKey === alertKey(openAlert)}
          error={noteError}
          onChange={(value) => setDrafts((prev) => ({ ...prev, [alertKey(openAlert)]: value }))}
          onSave={() => void saveNote(openAlert)}
          onClose={() => setOpenKey('')}
        />
      )}
    </div>
  )
}
