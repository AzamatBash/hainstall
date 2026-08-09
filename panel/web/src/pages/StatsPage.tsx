import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  api,
  type AnalyticsNode,
  type RemnaPanel,
  type WeekAnalytics,
  translateError,
} from '../api'
import BrandNav from '../components/BrandNav'
import OnlineUsersChart, { type OnlinePoint } from '../components/OnlineUsersChart'

type HoursPreset = 1 | 24 | 168 | 744
type Mode = 'stats' | 'analytics'
type AnalyticsTab = 'week' | 'nodes'

const PRESETS: { hours: HoursPreset; label: string }[] = [
  { hours: 1, label: '1 час' },
  { hours: 24, label: '24 часа' },
  { hours: 168, label: '7 дней' },
  { hours: 744, label: '30 дней' },
]

const PROTO_OPTS = [
  '',
  'vless_reality',
  'vless',
  'hysteria2',
  'shadowsocks',
  'trojan',
  'wireguard',
  'other',
  'unknown',
]

function formatAt(iso: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return (
    d.toLocaleString('ru-RU', {
      timeZone: 'Europe/Moscow',
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }) + ' (МСК)'
  )
}

function sumLatestByKey(buckets: { t: number; key: string; online: number }[]) {
  const latestT = new Map<string, number>()
  const online = new Map<string, number>()
  for (const b of buckets) {
    const prev = latestT.get(b.key) ?? -1
    if (b.t >= prev) {
      latestT.set(b.key, b.t)
      online.set(b.key, b.online)
    }
  }
  return [...online.entries()]
    .map(([key, value]) => ({ key, online: value }))
    .sort((a, b) => b.online - a.online)
}

export default function StatsPage() {
  const [mode, setMode] = useState<Mode>('stats')
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

  const [analyticsTab, setAnalyticsTab] = useState<AnalyticsTab>('week')
  const [nodes, setNodes] = useState<AnalyticsNode[]>([])
  const [week, setWeek] = useState<WeekAnalytics | null>(null)
  const [analyticsLoading, setAnalyticsLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [savingKey, setSavingKey] = useState('')

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

  const loadAnalytics = useCallback(async () => {
    setAnalyticsLoading(true)
    setError('')
    try {
      const [nodesRes, weekRes] = await Promise.all([
        api<{ nodes: AnalyticsNode[] }>('/api/analytics/nodes'),
        api<WeekAnalytics>('/api/analytics/week?hours=168'),
      ])
      setNodes(Array.isArray(nodesRes.nodes) ? nodesRes.nodes : [])
      setWeek(weekRes)
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setAnalyticsLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadPanels()
  }, [loadPanels])

  useEffect(() => {
    setZoom(null)
    if (mode === 'stats' && activeId) void loadOnline(activeId, hours)
  }, [mode, activeId, hours, loadOnline])

  useEffect(() => {
    if (mode === 'analytics') void loadAnalytics()
  }, [mode, loadAnalytics])

  useEffect(() => {
    const id = window.setInterval(() => {
      void loadPanels()
      if (mode === 'stats' && activeRef.current) void loadOnline(activeRef.current, hours)
      if (mode === 'analytics') void loadAnalytics()
    }, 60_000)
    return () => window.clearInterval(id)
  }, [mode, hours, loadPanels, loadOnline, loadAnalytics])

  const active = panels.find((p) => p.id === activeId) || null
  const displayPoints = useMemo(() => {
    if (!zoom) return points
    return points.filter((p) => p.t >= zoom.from && p.t <= zoom.to)
  }, [points, zoom])

  const segmentLatest = useMemo(() => {
    const labels: Record<string, string> = {
      vless_reality: 'VLESS Reality',
      hysteria2: 'Hysteria',
      vless_reality_hp_front: 'VLESS Reality + HP front',
      cdn: 'CDN',
    }
    const order = ['vless_reality', 'hysteria2', 'vless_reality_hp_front', 'cdn']
    const now = new Map<string, number>()
    for (const n of nodes) {
      if (!n.enabled_in_analytics) continue
      const proto = (n.protocol || n.protocol_derived || '').toLowerCase()
      const isVless = proto === 'vless_reality' || proto === 'vless'
      const isHy2 = proto === 'hysteria2' || proto === 'hysteria' || proto === 'hy2'
      const online = Number(n.users_online) || 0
      if (isVless && n.role_hp_front) now.set('vless_reality_hp_front', (now.get('vless_reality_hp_front') || 0) + online)
      else if (isVless) now.set('vless_reality', (now.get('vless_reality') || 0) + online)
      if (isHy2) now.set('hysteria2', (now.get('hysteria2') || 0) + online)
      if (n.role_cdn_back) now.set('cdn', (now.get('cdn') || 0) + online)
    }
    // Prefer live catalog; fall back to last hour bucket from samples.
    const sample = new Map(sumLatestByKey(week?.by_segment ?? []).map((r) => [r.key, r.online]))
    const useNow = nodes.some((n) => n.enabled_in_analytics)
    return order.map((key) => ({
      key,
      label: labels[key] || key,
      online: useNow ? (now.get(key) ?? 0) : (sample.get(key) ?? 0),
    }))
  }, [nodes, week])
  const protoLatest = useMemo(() => sumLatestByKey(week?.by_protocol ?? []), [week])
  const roleLatest = useMemo(() => sumLatestByKey(week?.by_role ?? []), [week])

  async function syncNodes() {
    setSyncing(true)
    setError('')
    try {
      const res = await api<{ nodes: AnalyticsNode[] }>('/api/analytics/nodes/sync', {
        method: 'POST',
        body: '{}',
      })
      setNodes(Array.isArray(res.nodes) ? res.nodes : [])
      const weekRes = await api<WeekAnalytics>('/api/analytics/week?hours=168')
      setWeek(weekRes)
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setSyncing(false)
    }
  }

  async function patchNode(n: AnalyticsNode, patch: Record<string, unknown>) {
    const key = `${n.panel_id}:${n.remna_uuid}`
    setSavingKey(key)
    setError('')
    try {
      const res = await api<{ node: AnalyticsNode }>(
        `/api/analytics/nodes/${encodeURIComponent(n.panel_id)}/${encodeURIComponent(n.remna_uuid)}`,
        { method: 'PATCH', body: JSON.stringify(patch) },
      )
      if (res.node) {
        setNodes((list) =>
          list.map((row) =>
            row.panel_id === n.panel_id && row.remna_uuid === n.remna_uuid ? res.node : row,
          ),
        )
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setSavingKey('')
    }
  }

  function roleLabel(key: string) {
    if (key === 'rn_front') return 'RN front'
    if (key === 'rn_back') return 'RN back'
    if (key === 'hp_front') return 'HP front'
    if (key === 'hp_back') return 'HP back'
    if (key === 'cdn_back') return 'CDN back'
    return key
  }

  return (
    <div className="shell">
      <header className="topbar">
        <BrandNav active="stats" />
        <div className="row">
          <button
            className="btn btn-sm"
            type="button"
            onClick={() => {
              if (mode === 'stats') void loadPanels()
              else void loadAnalytics()
            }}
          >
            Обновить
          </button>
        </div>
      </header>

      <section className="panel stats-panel">
        <nav className="page-tabs" aria-label="Режим">
          <button
            type="button"
            className={`page-tab${mode === 'stats' ? ' active' : ''}`}
            onClick={() => setMode('stats')}
          >
            Статистика
          </button>
          <button
            type="button"
            className={`page-tab${mode === 'analytics' ? ' active' : ''}`}
            onClick={() => setMode('analytics')}
          >
            Аналитика
          </button>
        </nav>

        {error && <p className="error">{error}</p>}

        {mode === 'stats' ? (
          <>
            <h2 style={{ marginTop: '0.75rem' }}>Онлайн пользователей</h2>
            <p className="muted stats-lead" style={{ marginTop: 0 }}>
              Сумма usersOnline по нодам Remnawave. Опрос раз в 5 минут, история до 31 дня.
            </p>
            {loading ? (
              <p className="muted">Загрузка…</p>
            ) : panels.length === 0 ? (
              <p className="muted">
                Панелей Remnawave пока нет — добавьте на <Link to="/">главной</Link> во вкладке
                «Панели Remnawave».
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
                        <button
                          className="btn btn-sm btn-ghost"
                          type="button"
                          onClick={() => setZoom(null)}
                        >
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
                    <p className="muted stats-zoom-hint" style={{ margin: 0, fontSize: '0.8rem' }}>
                      На телефоне: тап по графику. На ПК: наведение. Протяните пальцем/мышью — зум.
                    </p>
                  </div>
                )}
              </>
            )}
          </>
        ) : (
          <>
            <h2 style={{ marginTop: '0.75rem' }}>Аналитика</h2>
            <p className="muted stats-lead" style={{ marginTop: 0 }}>
              Консолидировано по всем нодам всех Remnawave-панелей. Протокол из inbound Remna; роли —
              вручную. Цифры — usersOnline как отдала панель.
            </p>

            <nav className="page-tabs" aria-label="Аналитика">
              <button
                type="button"
                className={`page-tab${analyticsTab === 'week' ? ' active' : ''}`}
                onClick={() => setAnalyticsTab('week')}
              >
                Неделя
              </button>
              <button
                type="button"
                className={`page-tab${analyticsTab === 'nodes' ? ' active' : ''}`}
                onClick={() => setAnalyticsTab('nodes')}
              >
                Ноды
              </button>
            </nav>

            {analyticsLoading && !week && nodes.length === 0 ? (
              <p className="muted">Загрузка…</p>
            ) : analyticsTab === 'week' ? (
              <div className="stack" style={{ marginTop: '0.75rem', gap: '1rem' }}>
                <div className="stats-current">
                  <div className="stats-current-value mono">{week?.total_online_now ?? 0}</div>
                  <div className="stats-current-meta muted">
                    <div>Онлайн сейчас · все панели</div>
                    <div>
                      Доля top-3:{' '}
                      <span className="mono">
                        {(week?.top3_share_pct ?? 0).toFixed(1)}%
                      </span>
                    </div>
                  </div>
                </div>

                <div>
                  <h3 style={{ margin: '0 0 0.4rem' }}>Сегменты (сейчас / последний час)</h3>
                  <p className="muted" style={{ marginTop: 0, fontSize: '0.8rem' }}>
                    VLESS Reality · Hysteria · VLESS Reality + HP front · CDN. Отметьте HP front / CDN back у нод.
                  </p>
                  <div className="table-wrap">
                    <table className="table">
                      <thead>
                        <tr>
                          <th>Сегмент</th>
                          <th>Online</th>
                        </tr>
                      </thead>
                      <tbody>
                        {segmentLatest.map((r) => (
                          <tr key={r.key}>
                            <td>{r.label}</td>
                            <td className="mono">{r.online}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  {protoLatest.length === 0 ? (
                    <p className="muted" style={{ marginTop: '0.5rem' }}>
                      Пока нет сэмплов — синхронизируйте ноды или подождите опрос.
                    </p>
                  ) : null}
                </div>

                <div>
                  <h3 style={{ margin: '0 0 0.4rem' }}>По роли (последний час)</h3>
                  {roleLatest.length === 0 ? (
                    <p className="muted">Отметьте роли у нод во вкладке «Ноды».</p>
                  ) : (
                    <div className="table-wrap">
                      <table className="table">
                        <thead>
                          <tr>
                            <th>Роль</th>
                            <th>Online</th>
                          </tr>
                        </thead>
                        <tbody>
                          {roleLatest.map((r) => (
                            <tr key={r.key}>
                              <td>{roleLabel(r.key)}</td>
                              <td className="mono">{r.online}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>

                <div>
                  <h3 style={{ margin: '0 0 0.4rem' }}>Топ нод</h3>
                  {(week?.top_nodes ?? []).length === 0 ? (
                    <p className="muted">Нет данных.</p>
                  ) : (
                    <div className="table-wrap">
                      <table className="table analytics-top-table">
                        <thead>
                          <tr>
                            <th>Панель</th>
                            <th>Нода</th>
                            <th>Протокол</th>
                            <th>Online</th>
                          </tr>
                        </thead>
                        <tbody>
                          {(week?.top_nodes ?? []).map((n) => (
                            <tr key={`${n.panel_id}:${n.remna_uuid}`}>
                              <td>{n.panel_name || n.panel_id.slice(0, 8)}</td>
                              <td className="analytics-cell-name" title={n.name}>{n.name}</td>
                              <td className="mono analytics-cell-proto">{n.protocol}</td>
                              <td className="mono">{n.online}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <div className="stack" style={{ marginTop: '0.75rem', gap: '0.75rem' }}>
                <div className="row" style={{ flexWrap: 'wrap', gap: '0.4rem' }}>
                  <button
                    className="btn btn-sm btn-primary"
                    type="button"
                    disabled={syncing}
                    onClick={() => void syncNodes()}
                  >
                    {syncing ? 'Синхронизация…' : 'Синхронизировать из Remnawave'}
                  </button>
                  <span className="muted" style={{ alignSelf: 'center', fontSize: '0.85rem' }}>
                    Нод: {nodes.length}
                  </span>
                </div>
                {nodes.length === 0 ? (
                  <p className="muted">Каталог пуст — синхронизируйте или дождитесь опроса (5 мин).</p>
                ) : (
                  <div className="table-wrap analytics-nodes-wrap">
                    <table className="table analytics-nodes-table">
                      <thead>
                        <tr>
                          <th>Панель</th>
                          <th>Нода</th>
                          <th>Online</th>
                          <th>Inbound</th>
                          <th>Протокол</th>
                          <th>Override</th>
                          <th>RN front</th>
                          <th>RN back</th>
                          <th>HP front</th>
                          <th>HP back</th>
                          <th>CDN back</th>
                          <th>В аналит.</th>
                        </tr>
                      </thead>
                      <tbody>
                        {nodes.map((n) => {
                          const key = `${n.panel_id}:${n.remna_uuid}`
                          const busy = savingKey === key
                          return (
                            <tr key={key} className={busy ? 'muted' : undefined}>
                              <td className="analytics-cell-panel">{n.panel_name || '—'}</td>
                              <td className="analytics-cell-name">
                                <div className="analytics-node-title" title={n.name}>{n.name}</div>
                                <div className="muted mono analytics-node-addr" title={n.address}>
                                  {n.address}
                                </div>
                              </td>
                              <td className="mono analytics-cell-online">
                                {n.users_online}
                                {!n.node_ok ? <span className="error"> · off</span> : null}
                              </td>
                              <td className="mono analytics-cell-inbound" title={n.inbound_tags || ''}>
                                {n.inbound_tags || '—'}
                              </td>
                              <td className="mono analytics-cell-proto">{n.protocol_derived || 'unknown'}</td>
                              <td className="analytics-cell-override">
                                <select
                                  value={n.protocol_override || ''}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { protocol_override: e.target.value })
                                  }
                                >
                                  {PROTO_OPTS.map((p) => (
                                    <option key={p || 'auto'} value={p}>
                                      {p || 'авто'}
                                    </option>
                                  ))}
                                </select>
                              </td>
                              <td className="analytics-cell-check">
                                <input
                                  type="checkbox"
                                  checked={n.role_rn_front}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { role_rn_front: e.target.checked })
                                  }
                                />
                              </td>
                              <td className="analytics-cell-check">
                                <input
                                  type="checkbox"
                                  checked={n.role_rn_back}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { role_rn_back: e.target.checked })
                                  }
                                />
                              </td>
                              <td className="analytics-cell-check">
                                <input
                                  type="checkbox"
                                  checked={!!n.role_hp_front}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { role_hp_front: e.target.checked })
                                  }
                                />
                              </td>
                              <td className="analytics-cell-check">
                                <input
                                  type="checkbox"
                                  checked={n.role_hp_back}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { role_hp_back: e.target.checked })
                                  }
                                />
                              </td>
                              <td className="analytics-cell-check">
                                <input
                                  type="checkbox"
                                  checked={!!n.role_cdn_back}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { role_cdn_back: e.target.checked })
                                  }
                                />
                              </td>
                              <td className="analytics-cell-check">
                                <input
                                  type="checkbox"
                                  checked={n.enabled_in_analytics}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { enabled_in_analytics: e.target.checked })
                                  }
                                />
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </section>
    </div>
  )
}
