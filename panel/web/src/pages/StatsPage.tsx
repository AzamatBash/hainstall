import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type RemnaPanel, translateError } from '../api'
import BrandNav from '../components/BrandNav'
import OnlineUsersChart, { type OnlinePoint } from '../components/OnlineUsersChart'

type HoursPreset = 1 | 24 | 168 | 744

const PRESETS: { hours: HoursPreset; label: string }[] = [
  { hours: 1, label: '1 час' },
  { hours: 24, label: '24 часа' },
  { hours: 168, label: '7 дней' },
  { hours: 744, label: '30 дней' },
]

export default function StatsPage() {
  const [panels, setPanels] = useState<RemnaPanel[]>([])
  const [activeId, setActiveId] = useState('')
  const [hours, setHours] = useState<HoursPreset>(24)
  const [points, setPoints] = useState<OnlinePoint[]>([])
  const [current, setCurrent] = useState<number | null>(null)
  const [onlineAt, setOnlineAt] = useState('')
  const [onlineError, setOnlineError] = useState('')
  const [zoom, setZoom] = useState<{ from: number; to: number } | null>(null)
  const [loading, setLoading] = useState(true)
  const [chartLoading, setChartLoading] = useState(false)
  const [error, setError] = useState('')
  const activeRef = useRef(activeId)
  activeRef.current = activeId

  const loadPanels = useCallback(async () => {
    setError('')
    try {
      const res = await api<{ panels: RemnaPanel[] }>('/api/remna-panels')
      const list = Array.isArray(res.panels) ? res.panels : []
      setPanels(list)
      setActiveId((cur) => {
        if (cur && list.some((p) => p.id === cur)) return cur
        return list[0]?.id || ''
      })
      setLoading(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
      setLoading(false)
    }
  }, [])

  const loadOnline = useCallback(async (panelId: string, windowHours: number) => {
    if (!panelId) {
      setPoints([])
      setCurrent(null)
      setOnlineAt('')
      setOnlineError('')
      return
    }
    setChartLoading(true)
    setError('')
    try {
      const res = await api<{
        points: OnlinePoint[]
        current?: number
        online_at?: string
        online_error?: string
      }>(`/api/remna-panels/${encodeURIComponent(panelId)}/online?hours=${windowHours}`)
      setPoints(Array.isArray(res.points) ? res.points : [])
      setCurrent(typeof res.current === 'number' ? res.current : null)
      setOnlineAt(res.online_at || '')
      setOnlineError(res.online_error || '')
      setPanels((list) =>
        list.map((p) =>
          p.id === panelId
            ? {
                ...p,
                online: typeof res.current === 'number' ? res.current : p.online,
                online_at: res.online_at || p.online_at,
                online_error: res.online_error || undefined,
              }
            : p,
        ),
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setChartLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadPanels()
  }, [loadPanels])

  useEffect(() => {
    setZoom(null)
    if (activeId) void loadOnline(activeId, hours)
  }, [activeId, hours, loadOnline])

  useEffect(() => {
    const id = window.setInterval(() => {
      void loadPanels()
      if (activeRef.current) void loadOnline(activeRef.current, hours)
    }, 60_000)
    return () => window.clearInterval(id)
  }, [hours, loadPanels, loadOnline])

  const active = panels.find((p) => p.id === activeId) || null
  const displayPoints = useMemo(() => {
    if (!zoom) return points
    return points.filter((p) => p.t >= zoom.from && p.t <= zoom.to)
  }, [points, zoom])

  function formatAt(iso: string) {
    if (!iso) return ''
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    return d.toLocaleString('ru-RU', {
      timeZone: 'Europe/Moscow',
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }) + ' (МСК)'
  }

  return (
    <div className="shell">
      <header className="topbar">
        <BrandNav active="stats" />
        <div className="row">
          <button className="btn btn-sm" type="button" onClick={() => void loadPanels()}>
            Обновить
          </button>
        </div>
      </header>

      <section className="panel stats-panel">
        <h2 style={{ marginTop: 0 }}>Онлайн пользователей</h2>
        <p className="muted" style={{ marginTop: 0 }}>
          Сумма usersOnline по нодам Remnawave. Опрос раз в 5 минут, история до 31 дня.
        </p>
        {error && <p className="error">{error}</p>}
        {loading ? (
          <p className="muted">Загрузка…</p>
        ) : panels.length === 0 ? (
          <p className="muted">
            Панелей Remnawave пока нет — добавьте на{' '}
            <Link to="/">главной</Link> во вкладке «Панели Remnawave».
          </p>
        ) : (
          <>
            <nav className="page-tabs stats-panel-tabs" aria-label="Панели Remnawave">
              {panels.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className={`page-tab${activeId === p.id ? ' active' : ''}`}
                  onClick={() => setActiveId(p.id)}
                  title={p.base_url}
                >
                  {p.name}
                  {typeof p.online === 'number' ? (
                    <span className="stats-tab-online mono">{p.online}</span>
                  ) : null}
                </button>
              ))}
            </nav>

            {active && (
              <div className="stack stats-body">
                <div className="stats-current">
                  <div className="stats-current-value mono">
                    {current != null ? current : onlineError ? '—' : '…'}
                  </div>
                  <div className="stats-current-meta muted">
                    <div>Онлайн пользователей · {active.name}</div>
                    {onlineAt ? <div>Опрос: {formatAt(onlineAt)}</div> : null}
                    {onlineError ? <div className="error">{onlineError}</div> : null}
                  </div>
                </div>

                <div className="row stats-presets" style={{ flexWrap: 'wrap', gap: '0.4rem' }}>
                  {PRESETS.map((p) => (
                    <button
                      key={p.hours}
                      type="button"
                      className={`btn btn-sm${hours === p.hours ? ' btn-primary' : ''}`}
                      onClick={() => setHours(p.hours)}
                    >
                      {p.label}
                    </button>
                  ))}
                  {zoom && (
                    <button className="btn btn-sm btn-ghost" type="button" onClick={() => setZoom(null)}>
                      Сбросить зум
                    </button>
                  )}
                </div>

                {chartLoading && points.length === 0 ? (
                  <p className="muted">Загрузка графика…</p>
                ) : (
                  <OnlineUsersChart
                    points={displayPoints}
                    hours={hours}
                    onZoom={(from, to) => setZoom({ from, to })}
                  />
                )}
                <p className="muted" style={{ margin: 0, fontSize: '0.8rem' }}>
                  Наведите на график — плашка с датой, временем (МСК) и онлайном. Зажмите и
                  протяните — зум по участку.
                </p>
              </div>
            )}
          </>
        )}
      </section>
    </div>
  )
}
