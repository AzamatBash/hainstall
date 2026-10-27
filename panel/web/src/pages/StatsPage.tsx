import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link, useSearchParams } from 'react-router-dom'
import TrafficMarks from '../components/TrafficMarks'
import {
  api,
  formatBitrateShort,
  type AnalyticsNode,
  type RemnaPanel,
  type WeekAnalytics,
  translateError,
} from '../api'
import OnlineUsersChart, { type MetricPoint, type OnlinePoint } from '../components/OnlineUsersChart'
import SegmentWeekChart, { seriesFromBuckets } from '../components/SegmentWeekChart'
import TrafficMirrorChart, { type TrafficPoint } from '../components/TrafficMirrorChart'

type HoursPreset = 1 | 6 | 12 | 24 | 168 | 744
type Mode = 'stats' | 'analytics'
type AnalyticsTab = 'week' | 'nodes'
type StatsScope = 'panel' | 'nodes'

type StatsNode = {
  panel_id: string
  panel_name?: string
  remna_uuid: string
  name: string
  address: string
  protocol?: string
  users_online: number
  node_ok: boolean
  down_bps?: number
  up_bps?: number
  traffic_at?: string
  traffic_drop?: boolean
  traffic_drop_hint?: string
  traffic_decline?: boolean
  traffic_decline_hint?: string
  last_seen_at?: number
}

function nodeKey(n: { panel_id: string; remna_uuid: string }) {
  return `${n.panel_id}:${n.remna_uuid}`
}

const PRESETS: { hours: HoursPreset; label: string }[] = [
  { hours: 1, label: '1 час' },
  { hours: 6, label: '6 часов' },
  { hours: 12, label: '12 часов' },
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

const HISTORY_HOURS = 744
const SAME_TIME_TOLERANCE_MIN = 10

type CompareSource = 'online' | 'traffic'

type CompareRow = {
  key: string
  label: string
  selected: boolean
  online: number | null
  down: number | null
  up: number | null
}

function mskClock(ts: number) {
  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Europe/Moscow',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(new Date(ts))
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? ''
  let hour = Number(get('hour'))
  if (hour === 24) hour = 0
  return {
    key: `${get('year')}-${get('month')}-${get('day')}`,
    minutes: hour * 60 + Number(get('minute')),
  }
}

function minuteDelta(a: number, b: number) {
  const d = Math.abs(a - b)
  return Math.min(d, 1440 - d)
}

function indexByDay<T extends { t: number }>(points: T[]) {
  const map = new Map<string, { p: T; minutes: number }[]>()
  for (const p of points) {
    const clock = mskClock(p.t)
    const row = { p, minutes: clock.minutes }
    const list = map.get(clock.key)
    if (list) list.push(row)
    else map.set(clock.key, [row])
  }
  return map
}

function closestInDay<T extends { t: number }>(
  map: Map<string, { p: T; minutes: number }[]>,
  day: string,
  minutes: number,
): T | null {
  const list = map.get(day)
  if (!list) return null
  let best: T | null = null
  let bestD = SAME_TIME_TOLERANCE_MIN + 1
  for (const row of list) {
    const d = minuteDelta(row.minutes, minutes)
    if (d < bestD) {
      best = row.p
      bestD = d
    }
  }
  return bestD <= SAME_TIME_TOLERANCE_MIN ? best : null
}

function dayLabel(key: string) {
  const [y, m, d] = key.split('-').map(Number)
  const date = new Date(Date.UTC(y, m - 1, d, 12))
  const weekday = date.toLocaleDateString('ru-RU', { weekday: 'short', timeZone: 'UTC' })
  return `${String(d).padStart(2, '0')}.${String(m).padStart(2, '0')} ${weekday}`
}

function mskTimeLabel(ts: number) {
  return new Date(ts).toLocaleTimeString('ru-RU', {
    timeZone: 'Europe/Moscow',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function buildCompareRows(
  anchor: number,
  online: OnlinePoint[],
  traffic: TrafficPoint[],
): CompareRow[] {
  const target = mskClock(anchor)
  const onlineDays = indexByDay(online)
  const trafficDays = indexByDay(traffic)
  const days = new Set<string>([...onlineDays.keys(), ...trafficDays.keys()])
  const older = [...days].filter((key) => key < target.key).sort((a, b) => b.localeCompare(a))
  const ordered = days.has(target.key) ? [target.key, ...older] : older
  return ordered
    .map((key) => {
      const sample = closestInDay(onlineDays, key, target.minutes)
      const rate = closestInDay(trafficDays, key, target.minutes)
      return {
        key,
        label: dayLabel(key),
        selected: key === target.key,
        online: sample ? sample.online : null,
        down: rate ? rate.down_bps : null,
        up: rate ? rate.up_bps : null,
      }
    })
    .filter((row) => row.online != null || row.down != null || row.up != null)
}

function perUserRate(down: number | null, online: number | null) {
  if (down == null || online == null) return null
  if (!Number.isFinite(down) || !Number.isFinite(online) || online <= 0) return null
  return down / online
}

function onlineDelta(value: number | null, base: number | null) {
  if (value == null || base == null) return null
  return Math.round(value - base)
}

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

function StatsTitleHelp({ title, help }: { title: string; help: string }) {
  return (
    <h2 className="stats-section-title">
      <span>{title}</span>
      <span className="hint-wrap" tabIndex={0}>
        <button
          type="button"
          className="icon-btn hint-btn stats-help-btn"
          aria-label={`Подсказка: ${title}`}
        >
          ?
        </button>
        <span className="hint-pop stats-help-pop" role="tooltip">
          {help}
        </span>
      </span>
    </h2>
  )
}

export default function StatsPage() {
  const [mode, setMode] = useState<Mode>('stats')
  const [statsScope, setStatsScope] = useState<StatsScope>('panel')
  const [panels, setPanels] = useState<RemnaPanel[]>([])
  const [activeId, setActiveId] = useState('')
  const [statsNodes, setStatsNodes] = useState<StatsNode[]>([])
  const [selectedNodeKey, setSelectedNodeKey] = useState('')
  const [nodeQuery, setNodeQuery] = useState('')
  const [onlineHours, setOnlineHours] = useState<HoursPreset>(24)
  const [trafficHours, setTrafficHours] = useState<HoursPreset>(24)
  const [points, setPoints] = useState<OnlinePoint[]>([])
  const [current, setCurrent] = useState<number | null>(null)
  const [onlineAt, setOnlineAt] = useState('')
  const [onlineError, setOnlineError] = useState('')
  const [zoom, setZoom] = useState<{ from: number; to: number } | null>(null)
  const [trafficZoom, setTrafficZoom] = useState<{ from: number; to: number } | null>(null)
  const [trafficPoints, setTrafficPoints] = useState<TrafficPoint[]>([])
  const [trafficDown, setTrafficDown] = useState<number | null>(null)
  const [trafficUp, setTrafficUp] = useState<number | null>(null)
  const [trafficAt, setTrafficAt] = useState('')
  const [trafficError, setTrafficError] = useState('')
  const [loading, setLoading] = useState(true)
  const [onlineLoading, setOnlineLoading] = useState(false)
  const [trafficLoading, setTrafficLoading] = useState(false)
  const [error, setError] = useState('')

  const [analyticsTab, setAnalyticsTab] = useState<AnalyticsTab>('week')
  const [nodes, setNodes] = useState<AnalyticsNode[]>([])
  const [week, setWeek] = useState<WeekAnalytics | null>(null)
  const [analyticsLoading, setAnalyticsLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [savingKey, setSavingKey] = useState('')
  const [pointMenu, setPointMenu] = useState<{
    t: number
    x: number
    y: number
    source: CompareSource
  } | null>(null)
  const [compare, setCompare] = useState<{
    anchor: number
    source: CompareSource
    loading: boolean
    error: string
    rows: CompareRow[]
  } | null>(null)

  const activeRef = useRef(activeId)
  activeRef.current = activeId
  const statsScopeRef = useRef(statsScope)
  statsScopeRef.current = statsScope
  const selectedNodeKeyRef = useRef(selectedNodeKey)
  selectedNodeKeyRef.current = selectedNodeKey
  const onlineHoursRef = useRef(onlineHours)
  onlineHoursRef.current = onlineHours
  const trafficHoursRef = useRef(trafficHours)
  trafficHoursRef.current = trafficHours

  const [searchParams] = useSearchParams()
  useEffect(() => {
    const node = searchParams.get('node')
    if (!node || !node.includes(':')) return
    setMode('stats')
    setStatsScope('nodes')
    setSelectedNodeKey(node)
  }, [searchParams])

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

  const loadOnlineSeries = useCallback(
    async (panelId: string, windowHours: number, remnaUUID?: string) => {
      if (!panelId || (remnaUUID !== undefined && !remnaUUID)) {
        setPoints([])
        setCurrent(null)
        setOnlineAt('')
        setOnlineError('')
        return
      }
      setOnlineLoading(true)
      setError('')
      try {
        const path = remnaUUID
          ? `/api/remna-panels/${encodeURIComponent(panelId)}/nodes/${encodeURIComponent(remnaUUID)}/online?hours=${windowHours}`
          : `/api/remna-panels/${encodeURIComponent(panelId)}/online?hours=${windowHours}`
        const onlineRes = await api<{
          points: OnlinePoint[]
          current?: number
          online_at?: string
          online_error?: string
        }>(path)
        setPoints(Array.isArray(onlineRes.points) ? onlineRes.points : [])
        setCurrent(typeof onlineRes.current === 'number' ? onlineRes.current : null)
        setOnlineAt(onlineRes.online_at || '')
        setOnlineError(onlineRes.online_error || '')
        if (!remnaUUID) {
          setPanels((list) =>
            list.map((p) =>
              p.id === panelId
                ? {
                    ...p,
                    online: typeof onlineRes.current === 'number' ? onlineRes.current : p.online,
                    online_at: onlineRes.online_at || p.online_at,
                    online_error: onlineRes.online_error || undefined,
                  }
                : p,
            ),
          )
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : translateError(String(err)))
      } finally {
        setOnlineLoading(false)
      }
    },
    [],
  )

  const loadTrafficSeries = useCallback(
    async (panelId: string, windowHours: number, remnaUUID?: string) => {
      if (!panelId || (remnaUUID !== undefined && !remnaUUID)) {
        setTrafficPoints([])
        setTrafficDown(null)
        setTrafficUp(null)
        setTrafficAt('')
        setTrafficError('')
        return
      }
      setTrafficLoading(true)
      setError('')
      try {
        const path = remnaUUID
          ? `/api/remna-panels/${encodeURIComponent(panelId)}/nodes/${encodeURIComponent(remnaUUID)}/traffic?hours=${windowHours}`
          : `/api/remna-panels/${encodeURIComponent(panelId)}/traffic?hours=${windowHours}`
        const trafficRes = await api<{
          points: TrafficPoint[]
          current_down_bps?: number
          current_up_bps?: number
          traffic_at?: string
          traffic_error?: string
        }>(path)
        setTrafficPoints(Array.isArray(trafficRes.points) ? trafficRes.points : [])
        setTrafficDown(typeof trafficRes.current_down_bps === 'number' ? trafficRes.current_down_bps : null)
        setTrafficUp(typeof trafficRes.current_up_bps === 'number' ? trafficRes.current_up_bps : null)
        setTrafficAt(trafficRes.traffic_at || '')
        setTrafficError(trafficRes.traffic_error || '')
        if (!remnaUUID) {
          setPanels((list) =>
            list.map((p) =>
              p.id === panelId
                ? {
                    ...p,
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
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : translateError(String(err)))
      } finally {
        setTrafficLoading(false)
      }
    },
    [],
  )

  const loadStatsNodes = useCallback(async () => {
    try {
      const res = await api<{ nodes: StatsNode[]; count?: number }>('/api/stats/nodes')
      const list = Array.isArray(res.nodes) ? res.nodes : []
      setStatsNodes(list)
      setSelectedNodeKey((cur) => {
        if (!cur) return cur
        return list.some((n) => nodeKey(n) === cur) ? cur : ''
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
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
    void loadStatsNodes()
  }, [loadPanels, loadStatsNodes])

  useEffect(() => {
    setPointMenu(null)
    setCompare(null)
  }, [activeId, selectedNodeKey, statsScope])

  useEffect(() => {
    if (!pointMenu) return
    function close() {
      setPointMenu(null)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setPointMenu(null)
    }
    window.addEventListener('pointerdown', close)
    window.addEventListener('keydown', onKey)
    window.addEventListener('scroll', close, true)
    return () => {
      window.removeEventListener('pointerdown', close)
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', close, true)
    }
  }, [pointMenu])

  useEffect(() => {
    setZoom(null)
    if (mode !== 'stats') return
    if (statsScope === 'panel' && activeId) {
      void loadOnlineSeries(activeId, onlineHours)
      return
    }
    if (statsScope === 'nodes' && selectedNodeKey) {
      const sep = selectedNodeKey.indexOf(':')
      const panelId = sep >= 0 ? selectedNodeKey.slice(0, sep) : ''
      const remnaUUID = sep >= 0 ? selectedNodeKey.slice(sep + 1) : ''
      if (panelId && remnaUUID) void loadOnlineSeries(panelId, onlineHours, remnaUUID)
    }
  }, [mode, statsScope, activeId, selectedNodeKey, onlineHours, loadOnlineSeries])

  useEffect(() => {
    setTrafficZoom(null)
    if (mode !== 'stats') return
    if (statsScope === 'panel' && activeId) {
      void loadTrafficSeries(activeId, trafficHours)
      return
    }
    if (statsScope === 'nodes' && selectedNodeKey) {
      const sep = selectedNodeKey.indexOf(':')
      const panelId = sep >= 0 ? selectedNodeKey.slice(0, sep) : ''
      const remnaUUID = sep >= 0 ? selectedNodeKey.slice(sep + 1) : ''
      if (panelId && remnaUUID) void loadTrafficSeries(panelId, trafficHours, remnaUUID)
    }
  }, [mode, statsScope, activeId, selectedNodeKey, trafficHours, loadTrafficSeries])

  useEffect(() => {
    if (mode === 'analytics') void loadAnalytics()
  }, [mode, loadAnalytics])

  useEffect(() => {
    const id = window.setInterval(() => {
      void loadPanels()
      void loadStatsNodes()
      if (mode === 'stats') {
        if (statsScopeRef.current === 'panel' && activeRef.current) {
          void loadOnlineSeries(activeRef.current, onlineHoursRef.current)
          void loadTrafficSeries(activeRef.current, trafficHoursRef.current)
        } else if (statsScopeRef.current === 'nodes' && selectedNodeKeyRef.current) {
          const key = selectedNodeKeyRef.current
          const sep = key.indexOf(':')
          const panelId = sep >= 0 ? key.slice(0, sep) : ''
          const remnaUUID = sep >= 0 ? key.slice(sep + 1) : ''
          if (panelId && remnaUUID) {
            void loadOnlineSeries(panelId, onlineHoursRef.current, remnaUUID)
            void loadTrafficSeries(panelId, trafficHoursRef.current, remnaUUID)
          }
        }
      }
      if (mode === 'analytics') void loadAnalytics()
    }, 60_000)
    return () => window.clearInterval(id)
  }, [mode, loadPanels, loadStatsNodes, loadOnlineSeries, loadTrafficSeries, loadAnalytics])

  const active = panels.find((p) => p.id === activeId) || null
  const selectedNode = statsNodes.find((n) => nodeKey(n) === selectedNodeKey) || null
  const filteredStatsNodes = useMemo(() => {
    const q = nodeQuery.trim().toLowerCase()
    if (!q) return statsNodes
    return statsNodes.filter((n) => {
      const hay = `${n.name} ${n.address} ${n.panel_name || ''} ${n.protocol || ''}`.toLowerCase()
      return hay.includes(q)
    })
  }, [statsNodes, nodeQuery])
  const chartSubject =
    statsScope === 'nodes' && selectedNode
      ? `Нода: ${selectedNode.name}${selectedNode.panel_name ? ` · ${selectedNode.panel_name}` : ''}.`
      : active
        ? `Панель: ${active.name}.`
        : ''
  const showCharts =
    mode === 'stats' &&
    ((statsScope === 'panel' && !!active) || (statsScope === 'nodes' && !!selectedNode))

  const displayPoints = useMemo((): MetricPoint[] => {
    const src = !zoom ? points : points.filter((p) => p.t >= zoom.from && p.t <= zoom.to)
    return src.map((p) => ({ t: p.t, value: p.online }))
  }, [points, zoom])

  const displayTrafficPoints = useMemo((): TrafficPoint[] => {
    const src = !trafficZoom
      ? trafficPoints
      : trafficPoints.filter((p) => p.t >= trafficZoom.from && p.t <= trafficZoom.to)
    return src.map((p) => ({
      t: p.t,
      down_bps: Number(p.down_bps) || 0,
      up_bps: Number(p.up_bps) || 0,
    }))
  }, [trafficPoints, trafficZoom])

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

  function openPointMenu(source: CompareSource, at: { t: number; clientX: number; clientY: number }) {
    const menuW = 168
    const menuH = 44
    setPointMenu({
      t: at.t,
      source,
      x: Math.max(8, Math.min(at.clientX, window.innerWidth - menuW - 8)),
      y: Math.max(8, Math.min(at.clientY, window.innerHeight - menuH - 8)),
    })
  }

  async function runCompare(anchor: number, source: CompareSource) {
    setPointMenu(null)
    const panelId = statsScope === 'nodes' ? selectedNode?.panel_id || '' : activeId
    const uuid = statsScope === 'nodes' ? selectedNode?.remna_uuid : undefined
    setCompare({ anchor, source, loading: true, error: '', rows: [] })
    if (!panelId || (statsScope === 'nodes' && !uuid)) {
      setCompare({ anchor, source, loading: false, error: 'Нет данных для сравнения', rows: [] })
      return
    }
    const onlinePath = uuid
      ? `/api/remna-panels/${encodeURIComponent(panelId)}/nodes/${encodeURIComponent(uuid)}/online?hours=${HISTORY_HOURS}&raw=1`
      : `/api/remna-panels/${encodeURIComponent(panelId)}/online?hours=${HISTORY_HOURS}&raw=1`
    const trafficPath = uuid
      ? `/api/remna-panels/${encodeURIComponent(panelId)}/nodes/${encodeURIComponent(uuid)}/traffic?hours=${HISTORY_HOURS}&raw=1`
      : `/api/remna-panels/${encodeURIComponent(panelId)}/traffic?hours=${HISTORY_HOURS}&raw=1`
    try {
      const [onlineRes, trafficRes] = await Promise.all([
        api<{ points: OnlinePoint[] }>(onlinePath),
        api<{ points: TrafficPoint[] }>(trafficPath),
      ])
      setCompare({
        anchor,
        source,
        loading: false,
        error: '',
        rows: buildCompareRows(
          anchor,
          Array.isArray(onlineRes.points) ? onlineRes.points : [],
          Array.isArray(trafficRes.points) ? trafficRes.points : [],
        ),
      })
    } catch (err) {
      setCompare({
        anchor,
        source,
        loading: false,
        error: err instanceof Error ? err.message : translateError(String(err)),
        rows: [],
      })
    }
  }

  function compareTable(source: CompareSource) {
    if (!compare || compare.source !== source) return null
    const base = compare.rows.find((row) => row.selected)?.online ?? null
    const others = compare.rows.filter((row) => !row.selected).length
    return (
      <div className="same-time">
        <div className="same-time-head">
          <strong>То же время · {mskTimeLabel(compare.anchor)} МСК</strong>
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setCompare(null)}>
            Закрыть
          </button>
        </div>
        {compare.loading ? <p className="muted">Считаю по всем дням…</p> : null}
        {compare.error ? <p className="error">{compare.error}</p> : null}
        {!compare.loading && !compare.error && compare.rows.length === 0 ? (
          <p className="muted">На это время в истории ничего нет.</p>
        ) : null}
        {!compare.loading && compare.rows.length > 0 ? (
          <>
            <div className="same-time-scroll">
              <table className="same-time-table">
                <thead>
                  <tr>
                    <th>День</th>
                    <th>Онлайн</th>
                    <th>Δ</th>
                    <th>Отдача TX</th>
                    <th>Загрузка RX</th>
                    <th title="Отдача TX на одного онлайн">На юзера</th>
                  </tr>
                </thead>
                <tbody>
                  {compare.rows.map((row) => {
                    const delta = row.selected ? null : onlineDelta(row.online, base)
                    return (
                      <tr key={row.key} className={row.selected ? 'is-selected' : ''}>
                        <td>
                          {row.label}
                          {row.selected ? <span className="same-time-mark">эта точка</span> : null}
                        </td>
                        <td className="mono">{row.online == null ? '—' : Math.round(row.online)}</td>
                        <td className={`mono${delta != null && delta > 0 ? ' up' : ''}${delta != null && delta < 0 ? ' down' : ''}`}>
                          {row.selected || delta == null ? '—' : delta > 0 ? `+${delta}` : String(delta)}
                        </td>
                        <td className="mono">{formatBitrateShort(row.down)}</td>
                        <td className="mono">{formatBitrateShort(row.up)}</td>
                        <td className="mono" title="Отдача TX / онлайн">
                          {formatBitrateShort(perUserRate(row.down, row.online))}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            {others === 0 ? <p className="muted">Других дней на это время в истории пока нет.</p> : null}
          </>
        ) : null}
      </div>
    )
  }

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
      {pointMenu
        ? createPortal(
            <div
              className="chart-context"
              style={{ left: pointMenu.x, top: pointMenu.y }}
              role="menu"
              onPointerDown={(e) => e.stopPropagation()}
            >
              <button
                type="button"
                role="menuitem"
                onClick={() => void runCompare(pointMenu.t, pointMenu.source)}
              >
                Сравнить
              </button>
            </div>,
            document.body,
          )
        : null}
      <header className="page-toolbar">
        <div className="page-toolbar-title">
          <h1>Статистика</h1>
        </div>
        <div className="page-toolbar-actions">
          <button
            className="btn btn-sm"
            type="button"
            onClick={() => {
              if (mode === 'stats') {
                void loadPanels()
                void loadStatsNodes()
              } else void loadAnalytics()
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
            {loading ? (
              <p className="muted">Загрузка…</p>
            ) : panels.length === 0 ? (
              <p className="muted">
                Панелей Remnawave пока нет — добавьте на <Link to="/nodes">нодах</Link> во вкладке
                «Панели Remnawave».
              </p>
            ) : (
              <>
                <div className="stats-panel-picker" role="group" aria-label="Панель Remnawave">
                  <div className="stats-panel-picker-label">
                    <span className="stats-panel-picker-kicker">Панель Remnawave</span>
                    <span className="muted stats-panel-picker-hint">
                      Общий фильтр для онлайн и трафика
                    </span>
                  </div>
                  <div className="stats-panel-picker-list">
                    {panels.map((p) => (
                      <button
                        key={p.id}
                        type="button"
                        className={`stats-panel-chip${statsScope === 'panel' && activeId === p.id ? ' active' : ''}`}
                        onClick={() => {
                          setStatsScope('panel')
                          setSelectedNodeKey('')
                          setActiveId(p.id)
                        }}
                        title={p.base_url}
                      >
                        <span className="stats-panel-chip-name">{p.name}</span>
                        {typeof p.online === 'number' ? (
                          <span className="stats-panel-chip-online mono">{p.online}</span>
                        ) : null}
                      </button>
                    ))}
                    <button
                      type="button"
                      className={`stats-panel-chip${statsScope === 'nodes' ? ' active' : ''}`}
                      onClick={() => {
                        setStatsScope('nodes')
                        setSelectedNodeKey('')
                        void loadStatsNodes()
                      }}
                      title="Статистика по нодам"
                    >
                      <span className="stats-panel-chip-name">Ноды</span>
                      <span className="stats-panel-chip-online mono">{statsNodes.length}</span>
                    </button>
                  </div>
                </div>

                {statsScope === 'nodes' && !selectedNode ? (
                  <div className="stack stats-body stats-nodes-list-wrap">
                    <div className="row stats-nodes-toolbar" style={{ flexWrap: 'wrap', gap: '0.5rem' }}>
                      <label className="nodes-search stats-nodes-search" title="Поиск по имени, адресу или панели">
                        <input
                          type="search"
                          placeholder="Поиск: имя, адрес, панель…"
                          value={nodeQuery}
                          onChange={(e) => setNodeQuery(e.target.value)}
                          aria-label="Поиск нод"
                        />
                      </label>
                      <span className="muted" style={{ alignSelf: 'center', fontSize: '0.85rem' }}>
                        {filteredStatsNodes.length}
                        {filteredStatsNodes.length !== statsNodes.length
                          ? ` из ${statsNodes.length}`
                          : ''}
                      </span>
                    </div>
                    {statsNodes.length === 0 ? (
                      <p className="muted">
                        Каталог нод пуст — дождитесь опроса Remnawave (раз в 5 минут) или
                        синхронизируйте во вкладке «Аналитика → Ноды».
                      </p>
                    ) : filteredStatsNodes.length === 0 ? (
                      <p className="muted">Ничего не найдено.</p>
                    ) : (
                      <div className="stats-nodes-list" role="list">
                        {filteredStatsNodes.map((n) => (
                          <button
                            key={nodeKey(n)}
                            type="button"
                            className="stats-node-row"
                            role="listitem"
                            onClick={() => setSelectedNodeKey(nodeKey(n))}
                          >
                            <span
                              className={`analytics-status-dot${n.node_ok ? ' ok' : ' bad'}`}
                              title={n.node_ok ? 'online' : 'offline'}
                              aria-hidden
                            />
                            <span className="stats-node-main">
                              <span className="stats-node-title">
                                <span className="stats-node-title-text">{n.name}</span>
                                <TrafficMarks
                                  drop={n.traffic_drop}
                                  decline={n.traffic_decline}
                                  dropHint={n.traffic_drop_hint}
                                  declineHint={n.traffic_decline_hint}
                                />
                              </span>
                              <span className="muted mono stats-node-meta">
                                {n.panel_name || '—'}
                                {n.address ? ` · ${n.address}` : ''}
                              </span>
                            </span>
                            <span className="stats-node-metrics mono">
                              <span title="Онлайн">{n.users_online}</span>
                              <span className="muted" title="TX / RX">
                                {formatBitrateShort(n.down_bps)} / {formatBitrateShort(n.up_bps)}
                              </span>
                            </span>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                ) : null}

                {showCharts && (
                  <div className="stack stats-body">
                    {statsScope === 'nodes' && selectedNode ? (
                      <div className="row stats-node-detail-bar" style={{ flexWrap: 'wrap', gap: '0.5rem' }}>
                        <button
                          type="button"
                          className="btn btn-sm btn-ghost"
                          onClick={() => setSelectedNodeKey('')}
                        >
                          ← К списку нод
                        </button>
                        <div className="stats-node-detail-title">
                          <strong>{selectedNode.name}</strong>
                          <TrafficMarks
                            drop={selectedNode.traffic_drop}
                            decline={selectedNode.traffic_decline}
                            dropHint={selectedNode.traffic_drop_hint}
                            declineHint={selectedNode.traffic_decline_hint}
                          />
                          <span className="muted mono" style={{ marginLeft: '0.5rem', fontSize: '0.85rem' }}>
                            {selectedNode.panel_name || ''}
                            {selectedNode.address ? ` · ${selectedNode.address}` : ''}
                          </span>
                        </div>
                      </div>
                    ) : null}

                    <StatsTitleHelp
                      title="Онлайн пользователей"
                      help={[
                        statsScope === 'nodes'
                          ? 'usersOnline этой ноды Remnawave. Опрос раз в 5 минут, история до 14 дней.'
                          : 'Сумма usersOnline по нодам Remnawave. Опрос раз в 5 минут, история до 31 дня.',
                        chartSubject,
                        onlineAt ? `Опрос: ${formatAt(onlineAt)}.` : '',
                      ]
                        .filter(Boolean)
                        .join(' ')}
                    />

                    {onlineError ? <div className="error">{onlineError}</div> : null}

                    <div className="row stats-presets" style={{ flexWrap: 'wrap', gap: '0.4rem' }}>
                      {PRESETS.map((p) => (
                        <button
                          key={p.hours}
                          type="button"
                          className={`btn btn-sm${onlineHours === p.hours ? ' btn-primary' : ''}`}
                          onClick={() => setOnlineHours(p.hours)}
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

                    {onlineLoading && points.length === 0 ? (
                      <p className="muted">Загрузка графика…</p>
                    ) : (
                      <OnlineUsersChart
                        points={displayPoints}
                        hours={onlineHours}
                        onZoom={(from, to) => setZoom({ from, to })}
                        onContextPoint={(at) => openPointMenu('online', at)}
                        valueLabel="Онлайн"
                        ariaLabel="Онлайн пользователей"
                        tapHint="Нажмите на график, чтобы увидеть дату и онлайн"
                        current={current}
                        showLegend
                      />
                    )}
                    {compareTable('online')}
                    <p className="muted stats-zoom-hint" style={{ margin: 0, fontSize: '0.8rem' }}>
                      На ПК: наведение, правый клик — сравнить это время с другими днями. Протяните мышью — зум.
                    </p>

                    <div className="stats-traffic-block">
                      <StatsTitleHelp
                        title={statsScope === 'nodes' ? 'Трафик ноды' : 'Общий трафик'}
                        help={[
                          statsScope === 'nodes'
                            ? 'Загрузка (RX) и отдача (TX) этой ноды. Опрос раз в 5 минут, история до 14 дней.'
                            : 'Сумма загрузки (RX) и отдачи (TX) по нодам Remnawave. Опрос раз в 5 минут, история до 31 дня.',
                          chartSubject,
                          trafficAt ? `Опрос: ${formatAt(trafficAt)}.` : '',
                        ]
                          .filter(Boolean)
                          .join(' ')}
                      />

                      {trafficError ? <div className="error">{trafficError}</div> : null}

                      <div className="row stats-presets" style={{ flexWrap: 'wrap', gap: '0.4rem' }}>
                        {PRESETS.map((p) => (
                          <button
                            key={`traffic-${p.hours}`}
                            type="button"
                            className={`btn btn-sm${trafficHours === p.hours ? ' btn-primary' : ''}`}
                            onClick={() => setTrafficHours(p.hours)}
                          >
                            {p.label}
                          </button>
                        ))}
                        {trafficZoom && (
                          <button
                            className="btn btn-sm btn-ghost"
                            type="button"
                            onClick={() => setTrafficZoom(null)}
                          >
                            Сбросить зум
                          </button>
                        )}
                      </div>

                      {trafficLoading && trafficPoints.length === 0 ? (
                        <p className="muted">Загрузка графика…</p>
                      ) : (
                        <TrafficMirrorChart
                          points={displayTrafficPoints}
                          hours={trafficHours}
                          size="stats"
                          onZoom={(from, to) => setTrafficZoom({ from, to })}
                          onContextPoint={(at) => openPointMenu('traffic', at)}
                          currentDownBps={trafficDown}
                          currentUpBps={trafficUp}
                        />
                      )}
                      {compareTable('traffic')}
                      <p className="muted stats-zoom-hint" style={{ margin: 0, fontSize: '0.8rem' }}>
                        На ПК: наведение, правый клик — сравнить это время с другими днями. Протяните мышью — зум.
                      </p>
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
