import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  api,
  formatBitrateShort,
  type AnalyticsNode,
  type RemnaPanel,
  type WeekAnalytics,
  translateError,
} from '../api'
import BrandNav from '../components/BrandNav'
import OnlineUsersChart, { type MetricPoint, type OnlinePoint } from '../components/OnlineUsersChart'
import SegmentWeekChart, { seriesFromBuckets } from '../components/SegmentWeekChart'
import TrafficMirrorChart, { type TrafficPoint } from '../components/TrafficMirrorChart'

type HoursPreset = 1 | 24 | 168 | 744
type Mode = 'stats' | 'analytics'
type AnalyticsTab = 'week' | 'nodes'

const PRESETS: { hours: HoursPreset; label: string }[] = [
  { hours: 1, label: '1 час' },
  { hours: 24, label: '24 часа' },
  { hours: 168, label: '7 дней' },
  { hours: 744, label: '30 дней' },
]

const PROTO_OPTS: { value: string; label: string }[] = [
  { value: 'vless_reality', label: 'VLESS Reality' },
  { value: 'hysteria2', label: 'Hysteria' },
  { value: 'vless', label: 'VLESS' },
  { value: 'shadowsocks', label: 'Shadowsocks' },
  { value: 'trojan', label: 'Trojan' },
  { value: 'wireguard', label: 'WireGuard' },
  { value: 'other', label: 'Other' },
  { value: 'unknown', label: 'Unknown' },
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

const SEGMENT_META: { key: string; label: string; color: string; hint: string }[] = [
  {
    key: 'vless_reality',
    label: 'VLESS Reality',
    color: '#5b8def',
    hint: 'VLESS Reality без HP back',
  },
  {
    key: 'hysteria2',
    label: 'Hysteria',
    color: '#3ecf8e',
    hint: 'Протокол Hysteria2',
  },
  {
    key: 'vless_reality_hp_front',
    label: 'VLESS Reality + HP front',
    color: '#f0b429',
    hint: 'VLESS Reality и роль HP back',
  },
  {
    key: 'cdn',
    label: 'CDN',
    color: '#c084fc',
    hint: 'Роль CDN back',
  },
]

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
  const [trafficPoints, setTrafficPoints] = useState<TrafficPoint[]>([])
  const [trafficDown, setTrafficDown] = useState<number | null>(null)
  const [trafficUp, setTrafficUp] = useState<number | null>(null)
  const [trafficAt, setTrafficAt] = useState('')
  const [trafficError, setTrafficError] = useState('')
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
      setTrafficPoints([])
      setTrafficDown(null)
      setTrafficUp(null)
      setTrafficAt('')
      setTrafficError('')
      return
    }
    setChartLoading(true)
    setError('')
    try {
      const [onlineRes, trafficRes] = await Promise.all([
        api<{
          points: OnlinePoint[]
          current?: number
          online_at?: string
          online_error?: string
        }>(`/api/remna-panels/${encodeURIComponent(panelId)}/online?hours=${windowHours}`),
        api<{
          points: TrafficPoint[]
          current_down_bps?: number
          current_up_bps?: number
          traffic_at?: string
          traffic_error?: string
        }>(`/api/remna-panels/${encodeURIComponent(panelId)}/traffic?hours=${windowHours}`),
      ])
      setPoints(Array.isArray(onlineRes.points) ? onlineRes.points : [])
      setCurrent(typeof onlineRes.current === 'number' ? onlineRes.current : null)
      setOnlineAt(onlineRes.online_at || '')
      setOnlineError(onlineRes.online_error || '')
      setTrafficPoints(Array.isArray(trafficRes.points) ? trafficRes.points : [])
      setTrafficDown(typeof trafficRes.current_down_bps === 'number' ? trafficRes.current_down_bps : null)
      setTrafficUp(typeof trafficRes.current_up_bps === 'number' ? trafficRes.current_up_bps : null)
      setTrafficAt(trafficRes.traffic_at || '')
      setTrafficError(trafficRes.traffic_error || '')
      setPanels((list) =>
        list.map((p) =>
          p.id === panelId
            ? {
                ...p,
                online: typeof onlineRes.current === 'number' ? onlineRes.current : p.online,
                online_at: onlineRes.online_at || p.online_at,
                online_error: onlineRes.online_error || undefined,
                down_bps:
                  typeof trafficRes.current_down_bps === 'number'
                    ? trafficRes.current_down_bps
                    : p.down_bps,
                up_bps:
                  typeof trafficRes.current_up_bps === 'number'
                    ? trafficRes.current_up_bps
                    : p.up_bps,
                traffic_at: trafficRes.traffic_at || p.traffic_at,
                traffic_error: trafficRes.traffic_error || undefined,
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
  const displayPoints = useMemo((): MetricPoint[] => {
    const src = !zoom ? points : points.filter((p) => p.t >= zoom.from && p.t <= zoom.to)
    return src.map((p) => ({ t: p.t, value: p.online }))
  }, [points, zoom])

  const displayTrafficPoints = useMemo((): TrafficPoint[] => {
    return trafficPoints.map((p) => ({
      t: p.t,
      down_bps: Number(p.down_bps) || 0,
      up_bps: Number(p.up_bps) || 0,
    }))
  }, [trafficPoints])

  const segmentSeries = useMemo(() => {
    const now = new Map<string, number>()
    for (const n of nodes) {
      if (!n.enabled_in_analytics) continue
      const proto = (n.protocol || n.protocol_derived || '').toLowerCase()
      const isVless = proto === 'vless_reality' || proto === 'vless'
      const isHy2 = proto === 'hysteria2' || proto === 'hysteria' || proto === 'hy2'
      const online = Number(n.users_online) || 0
      if (isVless && n.role_hp_back) {
        now.set('vless_reality_hp_front', (now.get('vless_reality_hp_front') || 0) + online)
      } else if (isVless) {
        now.set('vless_reality', (now.get('vless_reality') || 0) + online)
      }
      if (isHy2) now.set('hysteria2', (now.get('hysteria2') || 0) + online)
      if (n.role_cdn_back) now.set('cdn', (now.get('cdn') || 0) + online)
    }
    const buckets = week?.by_segment ?? []
    return SEGMENT_META.map((m) => ({
      key: m.key,
      label: m.label,
      color: m.color,
      hint: m.hint,
      onlineNow: now.get(m.key) ?? 0,
      points: seriesFromBuckets(buckets, m.key),
    }))
  }, [nodes, week])

  const segmentTotal = useMemo(
    () => segmentSeries.reduce((s, x) => s + x.onlineNow, 0),
    [segmentSeries],
  )
  const attributedNodes = useMemo(
    () =>
      nodes.filter((n) => {
        if (!n.enabled_in_analytics) return false
        const proto = (n.protocol || n.protocol_derived || '').toLowerCase()
        return (
          proto === 'vless_reality' ||
          proto === 'vless' ||
          proto === 'hysteria2' ||
          proto === 'hysteria' ||
          proto === 'hy2' ||
          n.role_cdn_back
        )
      }).length,
    [nodes],
  )

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
                        valueLabel="Онлайн"
                        ariaLabel="Онлайн пользователей"
                        tapHint="Нажмите на график, чтобы увидеть дату и онлайн"
                      />
                    )}
                    <p className="muted stats-zoom-hint" style={{ margin: 0, fontSize: '0.8rem' }}>
                      На телефоне: тап по графику. На ПК: наведение. Протяните пальцем/мышью — зум.
                    </p>

                    <div className="stats-traffic-block">
                      <h2 style={{ marginTop: '1.5rem', marginBottom: '0.35rem' }}>Общий трафик</h2>
                      <p className="muted stats-lead" style={{ marginTop: 0 }}>
                        Сумма загрузки (RX) и отдачи (TX) по нодам Remnawave. Тот же опрос раз в 5
                        минут, история до 31 дня.
                      </p>

                      <div className="stats-current stats-current-rates">
                        <div className="stats-rate">
                          <div className="stats-rate-label muted">Отдача TX</div>
                          <div className="stats-current-value mono stats-current-value-traffic">
                            {trafficDown != null
                              ? formatBitrateShort(trafficDown)
                              : trafficError
                                ? '—'
                                : '…'}
                          </div>
                        </div>
                        <div className="stats-rate">
                          <div className="stats-rate-label muted">Загрузка RX</div>
                          <div className="stats-current-value mono stats-current-value-traffic">
                            {trafficUp != null
                              ? formatBitrateShort(trafficUp)
                              : trafficError
                                ? '—'
                                : '…'}
                          </div>
                        </div>
                        <div className="stats-current-meta muted">
                          <div>Общий трафик · {active.name}</div>
                          {trafficAt ? <div>Опрос: {formatAt(trafficAt)}</div> : null}
                          {trafficError ? <div className="error">{trafficError}</div> : null}
                        </div>
                      </div>

                      <div className="row stats-presets" style={{ flexWrap: 'wrap', gap: '0.4rem' }}>
                        {PRESETS.map((p) => (
                          <button
                            key={`traffic-${p.hours}`}
                            type="button"
                            className={`btn btn-sm${hours === p.hours ? ' btn-primary' : ''}`}
                            onClick={() => setHours(p.hours)}
                          >
                            {p.label}
                          </button>
                        ))}
                      </div>

                      {chartLoading && trafficPoints.length === 0 ? (
                        <p className="muted">Загрузка графика…</p>
                      ) : (
                        <TrafficMirrorChart points={displayTrafficPoints} hours={hours} />
                      )}
                    </div>
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

            {analyticsLoading && nodes.length === 0 ? (
              <p className="muted">Загрузка…</p>
            ) : analyticsTab === 'week' ? (
              <div className="stack analytics-week" style={{ marginTop: '0.75rem', gap: '1rem' }}>
                <div className="stats-current">
                  <div className="stats-current-value mono">{segmentTotal}</div>
                  <div className="stats-current-meta muted">
                    <div>Онлайн в сегментах · все панели</div>
                    <div>
                      Нод с атрибутами: <span className="mono">{attributedNodes}</span> /{' '}
                      <span className="mono">{nodes.length}</span>
                    </div>
                  </div>
                </div>

                {nodes.length === 0 ? (
                  <p className="muted">
                    Нет нод — откройте вкладку «Ноды» и нажмите «Синхронизировать из Remnawave».
                  </p>
                ) : attributedNodes === 0 ? (
                  <p className="muted">
                    Сегменты пустые: на вкладке «Ноды» выставьте протокол и роли (HP back / CDN back).
                  </p>
                ) : null}

                <div className="analytics-segment-grid">
                  {segmentSeries.map((s) => {
                    const share = segmentTotal > 0 ? (s.onlineNow / segmentTotal) * 100 : 0
                    return (
                      <div key={s.key} className="analytics-segment-card">
                        <div className="analytics-segment-head">
                          <span
                            className="analytics-segment-swatch"
                            style={{ background: s.color }}
                            aria-hidden
                          />
                          <div>
                            <div className="analytics-segment-label">{s.label}</div>
                            <div className="muted analytics-segment-hint">{s.hint}</div>
                          </div>
                        </div>
                        <div className="analytics-segment-value mono">{s.onlineNow}</div>
                        <div className="muted analytics-segment-share mono">
                          {share.toFixed(1)}%
                        </div>
                      </div>
                    )
                  })}
                </div>

                <div>
                  <h3 style={{ margin: '0 0 0.5rem' }}>Неделя</h3>
                  <div className="analytics-segment-legend">
                    {segmentSeries.map((s) => (
                      <span key={s.key} className="analytics-segment-legend-item">
                        <span
                          className="analytics-segment-swatch"
                          style={{ background: s.color }}
                          aria-hidden
                        />
                        {s.label}
                      </span>
                    ))}
                  </div>
                  <SegmentWeekChart series={segmentSeries} />
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
                          <th>Протокол</th>
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
                          const protoValue =
                            n.protocol_override ||
                            n.protocol ||
                            n.protocol_derived ||
                            'unknown'
                          const known = PROTO_OPTS.some((p) => p.value === protoValue)
                          return (
                            <tr key={key} className={busy ? 'muted' : undefined}>
                              <td className="analytics-cell-panel">{n.panel_name || '—'}</td>
                              <td className="analytics-cell-name">
                                <div className="analytics-node-row">
                                  <div className="analytics-node-text">
                                    <div className="analytics-node-title" title={n.name}>
                                      {n.name}
                                    </div>
                                    <div className="muted mono analytics-node-addr" title={n.address}>
                                      {n.address}
                                    </div>
                                  </div>
                                  <span
                                    className={`analytics-status-dot${n.node_ok ? ' ok' : ' bad'}`}
                                    title={n.node_ok ? 'online' : 'offline'}
                                    aria-label={n.node_ok ? 'online' : 'offline'}
                                  />
                                </div>
                              </td>
                              <td className="analytics-cell-proto">
                                <select
                                  value={known ? protoValue : 'other'}
                                  disabled={busy}
                                  onChange={(e) =>
                                    void patchNode(n, { protocol_override: e.target.value })
                                  }
                                >
                                  {!known ? (
                                    <option value="other">{protoValue}</option>
                                  ) : null}
                                  {PROTO_OPTS.map((p) => (
                                    <option key={p.value} value={p.value}>
                                      {p.label}
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
