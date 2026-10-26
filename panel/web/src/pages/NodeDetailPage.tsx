import { FormEvent, Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  api,
  BackendRemnaLink,
  BackendServer,
  Entrance,
  flattenBackends,
  formatBitrateShort,
  formatBytes,
  formatUptime,
  isInternalBackend,
  Node,
  ProtectProfile,
  Provider,
  realServers,
  RemnaBackendStat,
  RemnaPanel,
  StatsSummary,
  statusLabel,
  SystemMetrics,
  translateError,
  userFacingStats,
} from '../api'
import { copyToClipboard, downloadTextFile } from '../clipboard'
import CountryPicker from '../components/CountryPicker'
import { countryLabel } from '../countries'
import { putNodeCache } from '../nodeCache'
import SparklineChart, { ChartPoint } from '../components/SparklineChart'
import TrafficMirrorChart, { type TrafficPoint } from '../components/TrafficMirrorChart'
import TrafficUsageChart, { type UsagePoint } from '../components/TrafficUsageChart'
import { ProviderBadge } from './NodesPage'

const HISTORY_MAX = 720
const POLL_MS = 5000
const AGENT_FETCH_MS = 10000
const TRAFFIC_HOURS_OPTIONS = [1, 2, 3, 6, 12, 24] as const
const TRAFFIC_HISTORY_MAX = (24 * 60 * 60) / 5
const MSK = 'Europe/Moscow'

function limitColor(pct?: number): string | undefined {
  if (pct == null || !Number.isFinite(pct)) return undefined
  if (pct >= 95) return 'var(--danger)'
  if (pct >= 70) return 'var(--warn)'
  return undefined
}

function fmtAvg(n?: number): string {
  if (n == null || !Number.isFinite(n)) return '—'
  return n.toFixed(2)
}

/** Timed fetch so a hung /stats cannot freeze the detail page. */
async function apiTimed<T = unknown>(path: string, ms = AGENT_FETCH_MS): Promise<T> {
  const ctrl = new AbortController()
  const t = setTimeout(() => ctrl.abort(), ms)
  try {
    return await api<T>(path, { signal: ctrl.signal })
  } finally {
    clearTimeout(t)
  }
}

function ymdMsk(d = new Date()): string {
  return d.toLocaleDateString('en-CA', { timeZone: MSK })
}

function shiftYmd(ymd: string, days: number): string {
  const ms = Date.parse(`${ymd}T12:00:00+03:00`) + days * 86_400_000
  return new Date(ms).toLocaleDateString('en-CA', { timeZone: MSK })
}

function mskRangeMs(fromYmd: string, toYmd: string): { from: number; to: number } {
  const a = fromYmd <= toYmd ? fromYmd : toYmd
  const b = fromYmd <= toYmd ? toYmd : fromYmd
  return {
    from: Date.parse(`${a}T00:00:00+03:00`),
    to: Date.parse(`${b}T23:59:59.999+03:00`),
  }
}

function pushPoint(prev: ChartPoint[], v: number, max = HISTORY_MAX): ChartPoint[] {
  const next = [...prev, { t: Date.now(), v }]
  return next.length > max ? next.slice(next.length - max) : next
}

function StatusBadge({ status, large }: { status: string; large?: boolean }) {
  const s = status || 'unknown'
  return (
    <span className={`badge badge-status ${s}${large ? ' badge-lg' : ''}`}>
      {statusLabel(s)}
    </span>
  )
}

function nodeHost(url: string): string {
  try {
    return new URL(url).hostname || url
  } catch {
    return url.replace(/^https?:\/\//, '').split(/[/:]/)[0] || url
  }
}

function CopyIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden>
      <rect x="9" y="9" width="11" height="11" rx="2" stroke="currentColor" strokeWidth="1.75" />
      <path
        d="M5 15V5a2 2 0 0 1 2-2h10"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M5 12.5l5 5L20 7"
        stroke="currentColor"
        strokeWidth="2.25"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function TrashIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function TrafficLogIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M4 19V5M4 19h16"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
      <path
        d="M8 15v-3M12 15V9M16 15v-6"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
    </svg>
  )
}

function EditIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M4 20h4l10.5-10.5a2.1 2.1 0 0 0-3-3L5 17v3zM13 7l3 3"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function RefreshIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M20 12a8 8 0 1 1-2.3-5.6"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
      <path
        d="M20 4v5h-5"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function ReloadIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M4 12a8 8 0 0 1 13.7-5.7M20 12a8 8 0 0 1-13.7 5.7"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
      <path
        d="M18 3v5h-5M6 21v-5h5"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function RestartIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M12 5v4M12 5a7 7 0 1 1-5.3 2.5"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M8 5H4v4"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

type Bundle = {
  token: string
  url: string
  host: string
  port: number
  agent_image: string
  commands: string
  files: Record<string, string>
}

export default function NodeDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [node, setNode] = useState<Node | null>(null)
  const [stats, setStats] = useState<StatsSummary | null>(null)
  const [system, setSystem] = useState<SystemMetrics | null>(null)
  const [cpuHist, setCpuHist] = useState<ChartPoint[]>([])
  const [memHist, setMemHist] = useState<ChartPoint[]>([])
  const [sessHist, setSessHist] = useState<ChartPoint[]>([])
  const [downHist, setDownHist] = useState<ChartPoint[]>([])
  const [upHist, setUpHist] = useState<ChartPoint[]>([])
  const [trafficPoints, setTrafficPoints] = useState<TrafficPoint[]>([])
  const [trafficHours, setTrafficHours] = useState<(typeof TRAFFIC_HOURS_OPTIONS)[number]>(1)
  const [usageFrom, setUsageFrom] = useState(() => shiftYmd(ymdMsk(), -6))
  const [usageTo, setUsageTo] = useState(() => ymdMsk())
  const [usagePoints, setUsagePoints] = useState<UsagePoint[]>([])
  const [usageTotals, setUsageTotals] = useState({ rx: 0, tx: 0 })
  const [usageLoading, setUsageLoading] = useState(false)
  const [downBps, setDownBps] = useState<number | null>(null)
  const [upBps, setUpBps] = useState<number | null>(null)
  const [backends, setBackends] = useState<BackendServer[]>([])
  const histResetRef = useRef(id)
  const trafficRef = useRef<{ t: number; inn: number; out: number } | null>(null)
  const proxySampleRef = useRef<{
    id: string
    byBackend: Record<string, { t: number; bin: number; bout: number }>
  }>({ id: '', byBackend: {} })
  const [proxyRates, setProxyRates] = useState<
    Record<string, { rx: number; tx: number } | null>
  >({})
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const [toast, setToast] = useState<{ kind: 'ok' | 'fail'; text: string } | null>(
    null,
  )
  const [showInstall, setShowInstall] = useState(false)
  const [installBundle, setInstallBundle] = useState<Bundle | null>(null)
  const [installFileKey, setInstallFileKey] = useState('docker-compose.yml')
  const [copied, setCopied] = useState('')
  const [editHost, setEditHost] = useState('')
  const [editPort, setEditPort] = useState('47893')
  const [editOpen, setEditOpen] = useState(false)
  const [editBusy, setEditBusy] = useState(false)
  const [entrancesBusy, setEntrancesBusy] = useState(false)
  const entrancesBusyRef = useRef(false)
  const [showAddEntrance, setShowAddEntrance] = useState(false)
  const [entranceDraft, setEntranceDraft] = useState({ port: '' })
  const [protectForm, setProtectForm] = useState<ProtectProfile>({
    client_hello: true,
    client_hello_delay_sec: 5,
    rate_limit: true,
    max_conn_per_ip: 100,
    max_rate_per_ip: 60,
    rate_window_sec: 10,
    ru_only: false,
    synproxy: false,
  })
  const [protectBusy, setProtectBusy] = useState(false)
  const [actionsOpen, setActionsOpen] = useState(false)
  const [showAddBackend, setShowAddBackend] = useState(false)
  const actionsRef = useRef<HTMLDivElement | null>(null)

  const [form, setForm] = useState({
    backend: 'app',
    name: '',
    address: '',
    port: '8443',
    weight: '100',
    balance: 'leastconn',
    client_ip: 'off',
    check: 'off',
  })

  const [remnaPanels, setRemnaPanels] = useState<RemnaPanel[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [remnaForms, setRemnaForms] = useState<
    Record<string, { panelId: string; remnaAddress: string }>
  >({})
  const [remnaLinked, setRemnaLinked] = useState<Record<string, boolean>>({})
  const [remnaStats, setRemnaStats] = useState<Record<string, RemnaBackendStat>>({})
  const [remnaBusyKey, setRemnaBusyKey] = useState('')

  const facing = useMemo(() => userFacingStats(stats), [stats])

  // HAProxy bin/bout are cumulative and both point at the client side:
  // bout is bytes sent to clients, bin is bytes received from them.
  // ↓ shows bout, ↑ shows bin.
  useEffect(() => {
    if (proxySampleRef.current.id !== id) {
      proxySampleRef.current = { id, byBackend: {} }
      setProxyRates({})
    }
    const rows = stats?.backends
    if (!id || !rows) return
    const now = Date.now()
    const prev = proxySampleRef.current.byBackend
    const nextSamples = { ...prev }
    const rates: Record<string, { rx: number; tx: number }> = {}
    let known = false
    for (const row of rows) {
      if (isInternalBackend(row.name)) continue
      if (row.bytes_in == null && row.bytes_out == null) continue
      const bin = Number(row.bytes_in) || 0
      const bout = Number(row.bytes_out) || 0
      const old = prev[row.name]
      nextSamples[row.name] = { t: now, bin, bout }
      known = true
      if (!old || now <= old.t) continue
      const dt = (now - old.t) / 1000
      if (dt < 0.5) continue
      if (bin < old.bin || bout < old.bout) continue
      rates[row.name] = {
        rx: (bout - old.bout) / dt,
        tx: (bin - old.bin) / dt,
      }
    }
    proxySampleRef.current = { id, byBackend: nextSamples }
    if (!known) return
    setProxyRates((cur) => {
      const merged = { ...cur }
      for (const name of Object.keys(nextSamples)) {
        if (!(name in merged)) merged[name] = null
      }
      for (const [name, rate] of Object.entries(rates)) merged[name] = rate
      return merged
    })
  }, [id, stats])
  const haproxyRows = useMemo(() => {
    const fronts = (stats?.frontends ?? []).filter((f) => !isInternalBackend(f.name))
    const rows: Array<{
      key: string
      name: string
      status: string
      sessions: number
      limit?: number
      queue?: number
      rate?: number
    }> = fronts.map((f) => ({
      key: `fe-${f.name}`,
      name: f.name,
      status: f.status,
      sessions: f.sessions,
      limit: f.limit,
      rate: f.rate ?? f.req_rate,
    }))
    for (const b of facing.backends) {
      rows.push({
        key: `be-${b.name}`,
        name: b.name,
        status: b.status,
        sessions: b.sessions,
        limit: b.limit,
        queue: b.queue,
        rate: undefined,
      })
    }
    return rows
  }, [stats, facing.backends])
  const haproxyShowLimit = haproxyRows.some((row) => (row.limit ?? 0) > 0)
  const haproxyShowQueue = haproxyRows.some((row) => row.queue != null)

  /** One HAProxy backend per listen port — used for NEW multi-port isolation only. */
  function backendForPort(port: number) {
    return `p${port}`
  }

  const nodeEntrances: Entrance[] = useMemo(() => {
    if (node?.entrances?.length) return node.entrances
    // Match agent/provision default: legacy listen_ports all map to backend "app".
    if (node?.listen_ports?.length) {
      return node.listen_ports.map((p) => ({ port: p, backend: 'app' }))
    }
    return [{ port: 8443, backend: 'app' }]
  }, [node])

  const servers = useMemo(() => realServers(backends), [backends])

  const serversByBackend = useMemo(() => {
    const map: Record<string, BackendServer[]> = {}
    for (const s of servers) {
      const be = s.backend || 'app'
      if (!map[be]) map[be] = []
      map[be].push(s)
    }
    return map
  }, [servers])

  /** Resolve servers for an entrance by its stored backend only. */
  function serversForEntrance(ent: Entrance): BackendServer[] {
    const primary = (ent.backend || 'app').trim() || 'app'
    const direct = serversByBackend[primary]
    if (direct?.length) return direct
    // Desync only: panel points at empty p{port}, servers still on app, and no entrance owns app.
    if (primary !== 'app' && (serversByBackend.app?.length ?? 0) > 0) {
      const appOwned = nodeEntrances.some((e) => (e.backend || 'app') === 'app')
      if (appOwned) return []
      const emptyPorts = nodeEntrances
        .filter((e) => !(serversByBackend[(e.backend || '').trim()]?.length))
        .sort((a, b) => a.port - b.port)
      if (emptyPorts[0]?.port === ent.port) return serversByBackend.app
    }
    return []
  }

  const orphanServers = useMemo(() => {
    const claimed = new Set<string>()
    for (const ent of nodeEntrances) {
      for (const s of serversForEntrance(ent)) {
        claimed.add(`${s.backend || 'app'}/${s.name}`)
      }
    }
    return servers.filter((s) => !claimed.has(`${s.backend || 'app'}/${s.name}`))
    // eslint-disable-next-line react-hooks/exhaustive-deps -- serversForEntrance closes over maps
  }, [servers, serversByBackend, nodeEntrances])

  const remnaKey = (backend: string, name: string) => `${backend}/${name}`

  const applyRemnaLinks = useCallback((links: BackendRemnaLink[]) => {
    const forms: Record<string, { panelId: string; remnaAddress: string }> = {}
    const linked: Record<string, boolean> = {}
    for (const l of links) {
      const k = remnaKey(l.backend, l.name)
      forms[k] = {
        panelId: l.remna_panel_id || '',
        remnaAddress: l.remna_address || '',
      }
      linked[k] = true
    }
    setRemnaForms((prev) => ({ ...prev, ...forms }))
    setRemnaLinked(linked)
  }, [])

  const loadRemnaPanels = useCallback(async () => {
    try {
      const res = await api<{ panels: RemnaPanel[] }>('/api/remna-panels')
      setRemnaPanels(Array.isArray(res.panels) ? res.panels : [])
    } catch {
      /* ignore — remna optional */
    }
  }, [])

  const loadProviders = useCallback(async () => {
    try {
      const res = await api<{ providers: Provider[] }>('/api/providers')
      setProviders(Array.isArray(res.providers) ? res.providers : [])
    } catch {
      /* ignore */
    }
  }, [])

  const loadRemnaLinks = useCallback(async () => {
    if (!id) return
    try {
      const res = await api<{ links: BackendRemnaLink[] }>(
        `/api/nodes/${id}/backends/remna-links`,
      )
      applyRemnaLinks(Array.isArray(res.links) ? res.links : [])
    } catch {
      /* ignore */
    }
  }, [id, applyRemnaLinks])

  const loadRemnaStats = useCallback(async () => {
    if (!id) return
    try {
      const res = await apiTimed<{ stats: RemnaBackendStat[] }>(
        `/api/nodes/${id}/backends/remna-stats`,
        15000,
      )
      const list = Array.isArray(res.stats) ? res.stats : []
      const map: Record<string, RemnaBackendStat> = {}
      for (const s of list) {
        map[remnaKey(s.backend, s.name)] = s
      }
      // Never blank the strip on a partial/empty answer while we already have numbers.
      setRemnaStats((prev) => {
        if (Object.keys(map).length === 0 && Object.keys(prev).length > 0) return prev
        return { ...prev, ...map }
      })
    } catch {
      /* ignore transient */
    }
  }, [id])

  const applyTraffic = useCallback((sys: SystemMetrics | null) => {
    if (!sys) return { downBps: null as number | null, upBps: null as number | null }
    const rx = Number(sys.net_rx_bytes) || 0
    const tx = Number(sys.net_tx_bytes) || 0
    if (rx <= 0 && tx <= 0) return { downBps: null, upBps: null }
    const now = Date.now()
    const prev = trafficRef.current
    let down: number | null = null
    let up: number | null = null
    if (prev && now > prev.t) {
      const dt = (now - prev.t) / 1000
      if (dt >= 0.5) {
        // Host NIC: RX = inbound, TX = outbound. Transit proxy ≈ symmetric.
        const inn = Math.max(0, (rx - prev.inn) / dt)
        const out = Math.max(0, (tx - prev.out) / dt)
        setUpBps(inn)
        setDownBps(out)
        setUpHist((h) => pushPoint(h, inn))
        setDownHist((h) => pushPoint(h, out))
        setTrafficPoints((prev) => {
          const next = [...prev, { t: now, down_bps: out, up_bps: inn }]
          const cutoff = now - trafficHours * 60 * 60 * 1000
          return next.filter((p) => p.t >= cutoff).slice(-TRAFFIC_HISTORY_MAX)
        })
        up = inn
        down = out
      }
    }
    trafficRef.current = { t: now, inn: rx, out: tx }
    return { downBps: down, upBps: up }
  }, [trafficHours])

  const load = useCallback(async () => {
    setError('')
    try {
      const list = await apiTimed<{ nodes: Node[] }>('/api/nodes', 15000)
      const found = list.nodes.find((n) => n.id === id) ?? null
      setNode(found)
      if (!found) {
        setError('Нода не найдена')
        return
      }
      try {
        const u = new URL(found.url)
        setEditHost(u.hostname)
        setEditPort(u.port || (u.protocol === 'https:' ? '443' : '80'))
      } catch {
        setEditHost('')
        setEditPort('47893')
      }
      // entrances come from node; no draft form
      if (found.protect) {
        setProtectForm({
          client_hello: found.protect.client_hello,
          client_hello_delay_sec: found.protect.client_hello_delay_sec || 5,
          rate_limit: found.protect.rate_limit,
          max_conn_per_ip: found.protect.max_conn_per_ip || 100,
          max_rate_per_ip: found.protect.max_rate_per_ip || 60,
          rate_window_sec: found.protect.rate_window_sec || 10,
          ru_only: found.protect.ru_only ?? false,
          synproxy: found.protect.synproxy ?? false,
        })
      }
      if (found.status !== 'online') {
        // Soft degrade: keep last backends/stats/remna so a transient offline
        // (port add / HAProxy recreate) does not blank the page.
        return
      }
      // Seed from list snapshot so a flaky stats call cannot blank the page.
      if (Array.isArray(found.live?.backends) && found.live.backends.length) {
        setBackends((prev) => (prev.length ? prev : realServers(found.live!.backends!)))
      }
      const settled = await Promise.allSettled([
        apiTimed<StatsSummary>(`/api/nodes/${id}/stats`),
        apiTimed<unknown>(`/api/nodes/${id}/backends`),
        apiTimed<SystemMetrics>(`/api/nodes/${id}/system`),
        apiTimed<{ version?: string }>(`/api/nodes/${id}/health`),
      ])
      const statsRes =
        settled[0].status === 'fulfilled' ? settled[0].value : null
      const backendsRes =
        settled[1].status === 'fulfilled' ? settled[1].value : null
      const systemRes =
        settled[2].status === 'fulfilled' ? settled[2].value : null
      const healthRes =
        settled[3].status === 'fulfilled' ? settled[3].value : null
      const loadErrs = settled
        .filter((r): r is PromiseRejectedResult => r.status === 'rejected')
        .map((r) => (r.reason instanceof Error ? r.reason.message : String(r.reason)))
      if (loadErrs.length && !backendsRes && !statsRes) {
        setError(loadErrs[0] || 'Ошибка загрузки')
      } else {
        setError('')
      }
      if (healthRes?.version) {
        setNode((cur) =>
          cur
            ? {
                ...cur,
                live: { ...(cur.live ?? {}), agent_version: healthRes.version },
              }
            : cur,
        )
      }
      if (statsRes) setStats(statsRes)
      let flatForCache: BackendServer[] = []
      if (backendsRes != null) {
        const flat = flattenBackends(backendsRes)
        setBackends((prev) => {
          const next = flat.length === 0 && prev.length > 0 ? prev : flat
          flatForCache = realServers(next)
          return next
        })
      }
      const facingNow = statsRes ? userFacingStats(statsRes) : null
      let metrics
      if (systemRes) {
        setSystem(systemRes)
        const rates = applyTraffic(systemRes)
        setCpuHist((h) => pushPoint(h, Number(systemRes.cpu_percent) || 0))
        setMemHist((h) => pushPoint(h, Number(systemRes.mem_percent) || 0))
        metrics = {
          cpu: Number(systemRes.cpu_percent) || 0,
          loadAvg: Array.isArray(systemRes.load_avg)
            ? systemRes.load_avg.map(Number)
            : undefined,
          downBps: rates.downBps,
          upBps: rates.upBps,
        }
      }
      if (flatForCache.length || metrics) {
        putNodeCache(id!, {
          backends: flatForCache,
          sessions:
            facingNow && facingNow.active_sessions !== null
              ? String(facingNow.active_sessions)
              : '—',
          metrics,
        })
      }
      if (facingNow?.active_sessions != null) {
        setSessHist((h) => pushPoint(h, facingNow.active_sessions ?? 0))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Ошибка загрузки')
    }
  }, [id, applyTraffic])

  useEffect(() => {
    if (histResetRef.current !== id) {
      histResetRef.current = id
      setCpuHist([])
      setMemHist([])
      setSessHist([])
      setDownHist([])
      setUpHist([])
      setTrafficPoints([])
      setDownBps(null)
      setUpBps(null)
      setUsagePoints([])
      setUsageTotals({ rx: 0, tx: 0 })
      trafficRef.current = null
      setSystem(null)
    }
  }, [id])

  useEffect(() => {
    if (!id) return
    let cancelled = false
    ;(async () => {
      try {
        const res = await api<{ points: TrafficPoint[] }>(
          `/api/nodes/${id}/traffic?hours=${trafficHours}`,
        )
        if (cancelled) return
        const pts = Array.isArray(res.points) ? res.points : []
        setTrafficPoints(pts)
        if (!pts.length) return
        // Sparklines stay compact: last hour of the fetched window.
        const sparkCut = Date.now() - 60 * 60 * 1000
        const spark = pts.filter((p) => p.t >= sparkCut)
        const sparkSrc = spark.length ? spark : pts.slice(-HISTORY_MAX)
        setDownHist(sparkSrc.map((p) => ({ t: p.t, v: p.down_bps })))
        setUpHist(sparkSrc.map((p) => ({ t: p.t, v: p.up_bps })))
        const last = pts[pts.length - 1]
        setDownBps(last.down_bps)
        setUpBps(last.up_bps)
      } catch {
        /* history optional */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [id, trafficHours])

  useEffect(() => {
    if (!id || !node?.traffic_log) {
      setUsagePoints([])
      setUsageTotals({ rx: 0, tx: 0 })
      return
    }
    let cancelled = false
    const { from, to } = mskRangeMs(usageFrom, usageTo)
    setUsageLoading(true)
    ;(async () => {
      try {
        const res = await api<{
          points?: Array<{ t: number; rx_bytes?: number; tx_bytes?: number }>
          total_rx_bytes?: number
          total_tx_bytes?: number
        }>(`/api/nodes/${id}/traffic?granularity=hour&from=${from}&to=${to}`)
        if (cancelled) return
        const pts = (Array.isArray(res.points) ? res.points : []).map((p) => ({
          t: p.t,
          rx_bytes: Number(p.rx_bytes) || 0,
          tx_bytes: Number(p.tx_bytes) || 0,
        }))
        setUsagePoints(pts)
        setUsageTotals({
          rx: Number(res.total_rx_bytes) || 0,
          tx: Number(res.total_tx_bytes) || 0,
        })
      } catch {
        if (!cancelled) {
          setUsagePoints([])
          setUsageTotals({ rx: 0, tx: 0 })
        }
      } finally {
        if (!cancelled) setUsageLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [id, node?.traffic_log, usageFrom, usageTo])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    void loadRemnaPanels()
    void loadProviders()
    void loadRemnaLinks()
  }, [loadRemnaPanels, loadProviders, loadRemnaLinks])

  // Live poll while detail page is open. Keep polling even if status flickers —
  // clearing the timer on offline used to freeze the page until hard refresh.
  useEffect(() => {
    if (!id) return
    let cancelled = false
    const tick = async () => {
      if (cancelled || entrancesBusyRef.current) return
      try {
        const settled = await Promise.allSettled([
          apiTimed<StatsSummary>(`/api/nodes/${id}/stats`),
          apiTimed<SystemMetrics>(`/api/nodes/${id}/system`),
          apiTimed<unknown>(`/api/nodes/${id}/backends`),
        ])
        if (cancelled || entrancesBusyRef.current) return
        const statsRes =
          settled[0].status === 'fulfilled' ? settled[0].value : null
        const systemRes =
          settled[1].status === 'fulfilled' ? settled[1].value : null
        const backendsRes =
          settled[2].status === 'fulfilled' ? settled[2].value : null
        if (statsRes) {
          setStats(statsRes)
          const facingNow = userFacingStats(statsRes)
          if (facingNow.active_sessions !== null) {
            setSessHist((h) => pushPoint(h, facingNow.active_sessions ?? 0))
          }
          putNodeCache(id, {
            sessions:
              facingNow.active_sessions !== null
                ? String(facingNow.active_sessions)
                : undefined,
          })
        }
        if (systemRes) {
          setSystem(systemRes)
          const rates = applyTraffic(systemRes)
          setCpuHist((h) => pushPoint(h, Number(systemRes.cpu_percent) || 0))
          setMemHist((h) => pushPoint(h, Number(systemRes.mem_percent) || 0))
          putNodeCache(id, {
            metrics: {
              cpu: Number(systemRes.cpu_percent) || 0,
              loadAvg: Array.isArray(systemRes.load_avg)
                ? systemRes.load_avg.map(Number)
                : undefined,
              downBps: rates.downBps,
              upBps: rates.upBps,
            },
          })
          // Successful poll ⇒ treat as online even if list briefly said offline.
          setNode((cur) =>
            cur && cur.status !== 'online' ? { ...cur, status: 'online' } : cur,
          )
        }
        if (backendsRes != null) {
          const flat = flattenBackends(backendsRes)
          setBackends((prev) => (flat.length === 0 && prev.length > 0 ? prev : flat))
        }
      } catch {
        /* ignore transient poll errors */
      }
    }
    void tick()
    const timer = setInterval(() => {
      void tick()
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [id, applyTraffic])

  // Remna stats poll (~5s) — keep going even if backends briefly empty.
  useEffect(() => {
    if (!id) return
    void loadRemnaStats()
    const timer = setInterval(() => {
      if (!entrancesBusyRef.current) void loadRemnaStats()
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [id, loadRemnaStats])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), 4500)
    return () => clearTimeout(t)
  }, [toast])

  const title = useMemo(() => node?.name ?? 'Нода', [node])
  const st = node?.status || 'unknown'

  useEffect(() => {
    if (!actionsOpen) return
    function onDoc(e: MouseEvent) {
      if (!actionsRef.current?.contains(e.target as globalThis.Node)) {
        setActionsOpen(false)
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setActionsOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [actionsOpen])

  const usageMin = shiftYmd(ymdMsk(), -89)
  const usageMax = ymdMsk()

  async function toggleTrafficLog() {
    if (!id || !node) return
    const next = !node.traffic_log
    setError('')
    setActionsOpen(false)
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ traffic_log: next }),
      })
      setNode((cur) => (cur ? { ...cur, ...res.node } : res.node))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось переключить подсчёт трафика')
    }
  }

  async function setCountry(country: string) {
    if (!id) return
    setError('')
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ country }),
      })
      setNode((cur) => (cur ? { ...cur, ...res.node } : res.node))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить страну')
    }
  }

  async function setRemnaPanel(panelId: string) {
    if (!id) return
    setError('')
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ remna_panel_id: panelId || null }),
      })
      setNode((cur) => (cur ? { ...cur, ...res.node } : res.node))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить Remnawave-панель')
    }
  }

  async function setProvider(providerId: string) {
    if (!id) return
    setError('')
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          provider_id: providerId || null,
          provider_account_id: null,
        }),
      })
      setNode((cur) => (cur ? { ...cur, ...res.node } : res.node))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить провайдера')
    }
  }

  async function setProviderAccount(accountId: string) {
    if (!id) return
    setError('')
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ provider_account_id: accountId || null }),
      })
      setNode((cur) => (cur ? { ...cur, ...res.node } : res.node))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить аккаунт провайдера')
    }
  }

  async function onConnect() {
    setConnecting(true)
    setError('')
    setToast(null)
    try {
      const res = await api<{
        ok: boolean
        online: boolean
        error?: string
        node: Node
      }>(`/api/nodes/${id}/connect`, { method: 'POST' })
      setNode(res.node)
      if (res.ok && res.online) {
        setError('')
        await load()
      } else {
        const text =
          'Нет связи: ' +
          (translateError(res.error || '') ||
            'нода недоступна — проверьте docker compose')
        setError(text)
      }
    } catch (err) {
      const text =
        'Нет связи: ' +
        (err instanceof Error ? err.message : 'ошибка проверки связи')
      setError(text)
    } finally {
      setConnecting(false)
    }
  }

  async function runAction(label: string, path: string, method = 'POST') {
    setBusy(true)
    setError('')
    setToast(null)
    try {
      const res = await api<{ ok?: boolean; action?: string; note?: string }>(path, {
        method,
      })
      const text =
        res?.note ||
        (label === 'Рестарт'
          ? 'HAProxy перезапущен'
          : label === 'Перезагрузка'
            ? 'Конфиг HAProxy перезагружен'
            : `${label}: готово`)
      setToast({ kind: 'ok', text })
      await load()
    } catch (err) {
      const text = err instanceof Error ? err.message : `${label}: ошибка`
      setError(text)
      setToast({ kind: 'fail', text })
    } finally {
      setBusy(false)
    }
  }

  async function putEntrances(next: Entrance[], okText: string) {
    entrancesBusyRef.current = true
    setEntrancesBusy(true)
    setError('')
    // Optimistic: keep existing servers on screen while agent reloads HAProxy.
    setNode((cur) =>
      cur
        ? {
            ...cur,
            entrances: next,
            listen_ports: next.map((e) => e.port),
          }
        : cur,
    )
    try {
      const res = await api<{
        ok: boolean
        saved?: boolean
        error?: string
        node: Node
        entrances?: Entrance[]
        agent?: { opened?: number[]; closed?: number[] }
      }>(`/api/nodes/${id}/entrances`, {
        method: 'PUT',
        body: JSON.stringify({ entrances: next }),
      })
      setNode(res.node)
      if (!res.ok) {
        setToast({
          kind: 'fail',
          text: res.error || 'Не удалось применить входы на VPS',
        })
      } else {
        const opened = res.agent?.opened?.length
          ? ` открыты: ${res.agent.opened.join(', ')}`
          : ''
        const closed = res.agent?.closed?.length
          ? ` закрыты: ${res.agent.closed.join(', ')}`
          : ''
        setToast({ kind: 'ok', text: `${okText}${opened}${closed}` })
      }
      // Soft refresh only — full load() used to race HAProxy recreate and wipe the page.
      try {
        const backendsRes = await apiTimed<unknown>(`/api/nodes/${id}/backends`, 15000)
        const flat = flattenBackends(backendsRes)
        setBackends((prev) => (flat.length === 0 && prev.length > 0 ? prev : flat))
      } catch {
        /* keep previous */
      }
      void loadRemnaStats()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить входы')
      // Re-sync entrances from server without clearing backends.
      try {
        const list = await apiTimed<{ nodes: Node[] }>('/api/nodes', 15000)
        const found = list.nodes.find((n) => n.id === id)
        if (found) setNode(found)
      } catch {
        /* ignore */
      }
    } finally {
      entrancesBusyRef.current = false
      setEntrancesBusy(false)
    }
  }

  async function onAddEntrance(e: FormEvent) {
    e.preventDefault()
    const port = Number(entranceDraft.port.trim())
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      setError('Порт входа: число 1–65535')
      return
    }
    if (nodeEntrances.some((x) => x.port === port)) {
      setError(`Порт ${port} уже занят`)
      return
    }
    // New port gets its own backend. Existing ports keep theirs — never rewrite them.
    const backend = backendForPort(port)
    setShowAddEntrance(false)
    setEntranceDraft({ port: '' })
    await putEntrances(
      [
        ...nodeEntrances.map((e) => ({
          port: e.port,
          backend: e.backend || 'app',
        })),
        { port, backend },
      ],
      `Вход :${port} создан`,
    )
  }

  async function onDeleteEntrance(port: number) {
    if (nodeEntrances.length <= 1) {
      setError('Нужен хотя бы один вход')
      return
    }
    const ent = nodeEntrances.find((x) => x.port === port)
    const list = ent ? serversForEntrance(ent) : []
    const be = list[0]?.backend || ent?.backend || backendForPort(port)
    if (
      !confirm(
        list.length
          ? `Удалить вход :${port} и ${list.length} цел(ей) только с этого порта?`
          : `Удалить вход :${port}? Порт закроется на VPS.`,
      )
    ) {
      return
    }
    setBusy(true)
    try {
      for (const s of list) {
        try {
          await api(
            `/api/nodes/${id}/backends/${encodeURIComponent(s.backend || be)}/${encodeURIComponent(s.name)}`,
            { method: 'DELETE' },
          )
        } catch {
          /* ignore */
        }
      }
    } finally {
      setBusy(false)
    }
    // Keep remaining entrance backends unchanged — do not remap to p{port}.
    await putEntrances(
      nodeEntrances
        .filter((x) => x.port !== port)
        .map((e) => ({ port: e.port, backend: e.backend || 'app' })),
      `Вход :${port} удалён`,
    )
  }

  function openAddServer(port: number) {
    setError('')
    const ent = nodeEntrances.find((x) => x.port === port)
    const backend = ent?.backend || 'app'
    const existing = servers.find((b) => b.backend === backend)
    setForm({
      backend,
      name: '',
      address: '',
      port: '8443',
      weight: '100',
      balance: existing?.balance ? String(existing.balance) : 'leastconn',
      client_ip: 'off',
      check: 'off',
    })
    setShowAddBackend(true)
  }

  async function onAddBackend(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api(`/api/nodes/${id}/backends`, {
        method: 'POST',
        body: JSON.stringify({
          backend: form.backend,
          name: form.name,
          address: form.address,
          port: Number(form.port),
          weight: Number(form.weight),
          balance: form.balance,
          client_ip: form.client_ip,
          check: form.check,
        }),
      })
      setForm((f) => ({ ...f, name: '', address: '' }))
      setShowAddBackend(false)
      setToast({ kind: 'ok', text: 'Цель добавлена на этот порт' })
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось добавить цель')
    } finally {
      setBusy(false)
    }
  }

  function closeAddBackend() {
    if (busy) return
    setShowAddBackend(false)
  }

  async function onClientIP(server: BackendServer, clientIP: string) {
    const prev = server.client_ip || 'off'
    if (clientIP === prev) return
    if (clientIP === 'proxy-ng') {
      if (!confirm('Включить proxy-ng? Nginx на цели должен принимать PROXY protocol на этом порту. Иначе соединения оборвутся.')) return
    }
    if (clientIP === 'proxy-vl') {
      if (!confirm('Включить proxy-vl? Xray на цели должен принимать PROXY protocol. Конфиг nginx сюда не подходит. Иначе соединения оборвутся.')) return
    }
    setBusy(true)
    setError('')
    try {
      await api(`/api/nodes/${id}/backends`, {
        method: 'POST',
        body: JSON.stringify({
          backend: server.backend,
          name: server.name,
          address: server.address,
          port: server.port,
          weight: server.weight || 100,
          balance: server.balance || '',
          client_ip: clientIP,
          check: server.check || 'off',
          check_port: server.check_port || 0,
        }),
      })
      setToast({ kind: 'ok', text: `${server.name}: ${clientIP === 'off' ? 'выкл' : clientIP}` })
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сменить режим IP')
    } finally {
      setBusy(false)
    }
  }

  async function onCheck(server: BackendServer, check: string) {
    const prev = server.check || 'off'
    if (check === prev) return
    if (check !== 'off') {
      if (
        !confirm(
          'Включить проверку? Она смотрит только открытый порт. Для Reality слепой ssl-hello помечает сервер как DOWN, и клиенты перестают попадать на него.',
        )
      ) {
        return
      }
    }
    setBusy(true)
    setError('')
    try {
      await api(`/api/nodes/${id}/backends`, {
        method: 'POST',
        body: JSON.stringify({
          backend: server.backend,
          name: server.name,
          address: server.address,
          port: server.port,
          weight: server.weight || 100,
          balance: server.balance || '',
          client_ip: server.client_ip || 'off',
          check,
          check_port: server.check_port || 0,
        }),
      })
      setToast({ kind: 'ok', text: `${server.name}: проверка ${check === 'off' ? 'выкл' : check}` })
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сменить проверку')
    } finally {
      setBusy(false)
    }
  }

  async function onDeleteBackend(backend: string, name: string) {
    if (!confirm(`Удалить сервер ${name} только с этого порта?`)) return
    setBusy(true)
    setError('')
    try {
      const res = await api<{ ok?: boolean; warning?: string }>(
        `/api/nodes/${id}/backends/${encodeURIComponent(backend)}/${encodeURIComponent(name)}`,
        { method: 'DELETE' },
      )
      if (res?.warning) {
        setToast({ kind: 'fail', text: res.warning })
      } else {
        setToast({ kind: 'ok', text: `Удалено: ${backend}/${name}` })
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Не удалось удалить'
      // Already gone on agent — treat as success so the UI can refresh.
      if (/not found|404/i.test(msg)) {
        setToast({ kind: 'ok', text: 'Уже удалено' })
      } else {
        setError(msg)
      }
    } finally {
      setBusy(false)
      try {
        await load()
      } catch {
        /* ignore refresh errors */
      }
    }
  }

  function remnaFormFor(backend: string, name: string) {
    const k = remnaKey(backend, name)
    return remnaForms[k] ?? { panelId: '', remnaAddress: '' }
  }

  function setRemnaForm(
    backend: string,
    name: string,
    patch: Partial<{ panelId: string; remnaAddress: string }>,
  ) {
    const k = remnaKey(backend, name)
    setRemnaForms((prev) => {
      const cur = prev[k] ?? { panelId: '', remnaAddress: '' }
      return {
        ...prev,
        [k]: { ...cur, ...patch },
      }
    })
  }

  async function onSaveRemna(backend: string, name: string) {
    const k = remnaKey(backend, name)
    const formState = remnaFormFor(backend, name)
    const panelId = formState.panelId.trim()
    const remnaAddress = formState.remnaAddress.trim()
    const clearing = !panelId || !remnaAddress
    setRemnaBusyKey(k)
    setError('')
    try {
      await api(
        `/api/nodes/${id}/backends/${encodeURIComponent(backend)}/${encodeURIComponent(name)}/remna`,
        {
          method: 'PUT',
          body: JSON.stringify({
            remna_panel_id: clearing ? null : panelId,
            remna_address: clearing ? null : remnaAddress,
          }),
        },
      )
      if (clearing) {
        setRemnaForms((prev) => ({
          ...prev,
          [k]: { panelId: '', remnaAddress: '' },
        }))
        setRemnaLinked((prev) => {
          const next = { ...prev }
          delete next[k]
          return next
        })
        setRemnaStats((prev) => {
          const next = { ...prev }
          delete next[k]
          return next
        })
      } else {
        setRemnaLinked((prev) => ({ ...prev, [k]: true }))
      }
      await loadRemnaLinks()
      await loadRemnaStats()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить Remna')
    } finally {
      setRemnaBusyKey('')
    }
  }

  async function onClearRemna(backend: string, name: string) {
    const k = remnaKey(backend, name)
    setRemnaBusyKey(k)
    setError('')
    try {
      await api(
        `/api/nodes/${id}/backends/${encodeURIComponent(backend)}/${encodeURIComponent(name)}/remna`,
        {
          method: 'PUT',
          body: JSON.stringify({ remna_panel_id: null, remna_address: null }),
        },
      )
      setRemnaForms((prev) => ({
        ...prev,
        [k]: { panelId: '', remnaAddress: '' },
      }))
      setRemnaLinked((prev) => {
        const next = { ...prev }
        delete next[k]
        return next
      })
      setRemnaStats((prev) => {
        const next = { ...prev }
        delete next[k]
        return next
      })
      await loadRemnaStats()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось отвязать Remna')
    } finally {
      setRemnaBusyKey('')
    }
  }

  async function onDeleteNode() {
    if (!node) return
    const ok = window.confirm(
      `Удалить ноду «${node.name}»?\n\nНода будет удалена из панели. Это действие нельзя отменить.`,
    )
    if (!ok) return
    try {
      await api(`/api/nodes/${id}`, { method: 'DELETE' })
      navigate('/nodes')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить')
    }
  }

  async function onRestartHaproxy() {
    const ok = window.confirm(
      'Перезапустить HAProxy на этой ноде?\n\nСоединения могут оборваться на несколько секунд.',
    )
    if (!ok) return
    await runAction('Рестарт', `/api/nodes/${id}/haproxy/restart`)
  }

  async function openInstall() {
    setError('')
    try {
      const res = await api<{ node: Node; bundle: Bundle }>(`/api/nodes/${id}/install`)
      setNode(res.node)
      setInstallBundle(res.bundle)
      setInstallFileKey('docker-compose.yml')
      setShowInstall(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось открыть установку')
    }
  }

  async function onSaveAddress(e: FormEvent) {
    e.preventDefault()
    setEditBusy(true)
    setError('')
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          host: editHost.trim(),
          port: Number(editPort) || 47893,
        }),
      })
      setNode(res.node)
      setEditOpen(false)
      setToast({ kind: 'ok', text: 'Адрес ноды обновлён' })
      setUpBps(null)
      setDownBps(null)
      trafficRef.current = null
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить адрес')
    } finally {
      setEditBusy(false)
    }
  }

  async function onSaveProtect(e: FormEvent) {
    e.preventDefault()
    setProtectBusy(true)
    setError('')
    try {
      const res = await api<{
        ok: boolean
        saved?: boolean
        error?: string
        node: Node
        protect?: ProtectProfile
      }>(`/api/nodes/${id}/protect`, {
        method: 'PUT',
        body: JSON.stringify(protectForm),
      })
      setNode(res.node)
      if (res.protect) setProtectForm({ ...protectForm, ...res.protect })
      if (!res.ok) {
        setToast({
          kind: 'fail',
          text: res.error || 'Защита сохранена в панели, но на VPS не применена',
        })
      } else {
        setToast({ kind: 'ok', text: 'Защита HAProxy применена на ноде' })
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить защиту')
    } finally {
      setProtectBusy(false)
    }
  }

  function proxyTraffic(backend: string) {
    if (!(backend in proxyRates)) return null
    const rate = proxyRates[backend]
    return (
      <span
        className="backend-proxy-net"
        title="Трафик клиентов через этот HAProxy. ↓ отдано клиентам, ↑ принято от клиентов."
      >
        <span className="label">прокси</span>
        <span className="node-live-net down" title="Отдано клиентам через этот прокси">
          <span className="node-live-arrow" aria-hidden>
            ↓
          </span>
          {formatBitrateShort(rate?.rx)}
        </span>
        <span className="node-live-net up" title="Принято от клиентов через этот прокси">
          <span className="node-live-arrow" aria-hidden>
            ↑
          </span>
          {formatBitrateShort(rate?.tx)}
        </span>
      </span>
    )
  }

  async function copyText(label: string, text: string) {
    const ok = await copyToClipboard(text)
    if (ok) {
      setCopied(label)
      setToast({ kind: 'ok', text: 'Скопировано' })
      setTimeout(() => setCopied(''), 1500)
    } else {
      setToast({ kind: 'fail', text: 'Не удалось скопировать' })
    }
  }

  return (
    <div className="shell">
      <header className="topbar node-detail-topbar">
        <div className="node-detail-title-row">
          <Link to="/nodes" className="btn btn-sm btn-ghost back-btn">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden>
              <path
                d="M15 6L9 12l6 6"
                stroke="currentColor"
                strokeWidth="1.75"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            Назад
          </Link>
          <div className="brand name-with-flag">
            {node && (
              <CountryPicker
                value={node.country || ''}
                onChange={(code) => void setCountry(code)}
              />
            )}
            <span>{title}</span>
          </div>
        </div>
        {node && (
          <div className="node-detail-meta">
            <span className="node-detail-country-inline muted">
              {node.country ? countryLabel(node.country) : 'Страна не задана'}
            </span>
            {node.provider_name ? (
              <ProviderBadge
                name={node.provider_name}
                favicon={node.provider_favicon}
                loginUrl={node.provider_login_url}
                accountLogin={node.provider_account_login}
              />
            ) : null}
            <span className="addr-cell">
              <span className="mono muted">{nodeHost(node.url)}</span>
              <button
                type="button"
                className={`icon-btn${copied === 'node-ip' ? ' copied' : ''}`}
                title={copied === 'node-ip' ? 'Скопировано' : 'Копировать IP'}
                aria-label={copied === 'node-ip' ? 'Скопировано' : 'Копировать IP'}
                onClick={() => void copyText('node-ip', nodeHost(node.url))}
              >
                {copied === 'node-ip' ? <CheckIcon /> : <CopyIcon />}
              </button>
            </span>
          </div>
        )}
        {node && (
          <div className="node-detail-selects">
            <div className="field node-remna-panel-field">
              <label htmlFor="node-remna-panel">Панель Remnawave</label>
              <select
                id="node-remna-panel"
                value={node.remna_panel_id || ''}
                onChange={(e) => void setRemnaPanel(e.target.value)}
              >
                <option value="">— не выбрана —</option>
                {remnaPanels.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="field node-remna-panel-field">
              <label htmlFor="node-provider">Провайдер</label>
              <select
                id="node-provider"
                value={node.provider_id || ''}
                onChange={(e) => void setProvider(e.target.value)}
              >
                <option value="">— не выбран —</option>
                {providers.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
            {node.provider_id ? (
              <div className="field node-remna-panel-field">
                <label htmlFor="node-provider-account">Аккаунт</label>
                <select
                  id="node-provider-account"
                  value={node.provider_account_id || ''}
                  onChange={(e) => void setProviderAccount(e.target.value)}
                >
                  <option value="">— не выбран —</option>
                  {(providers.find((p) => p.id === node.provider_id)?.accounts ?? []).map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.login}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}
          </div>
        )}
        <div className="node-detail-toolbar">
          <div className="actions-menu-wrap" ref={actionsRef}>
            <button
              className="btn btn-sm actions-menu-trigger"
              type="button"
              aria-expanded={actionsOpen}
              aria-haspopup="menu"
              disabled={busy || connecting}
              onClick={() => setActionsOpen((v) => !v)}
            >
              <span className="actions-menu-trigger-dots" aria-hidden>
                ⋯
              </span>
              Действия
            </button>
            {actionsOpen && (
              <div className="actions-menu" role="menu">
                <div className="actions-menu-label">Менеджмент</div>
                <button
                  className="actions-menu-item"
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    setActionsOpen(false)
                    setEditOpen((v) => !v)
                  }}
                >
                  <EditIcon />
                  {editOpen ? 'Скрыть адрес' : 'Изменить IP'}
                </button>
                <button
                  className="actions-menu-item"
                  type="button"
                  role="menuitem"
                  disabled={busy}
                  onClick={() => {
                    setActionsOpen(false)
                    void load()
                  }}
                >
                  <RefreshIcon />
                  Обновить
                </button>
                <button
                  className="actions-menu-item"
                  type="button"
                  role="menuitem"
                  disabled={busy || st !== 'online'}
                  onClick={() => {
                    setActionsOpen(false)
                    void runAction('Перезагрузка', `/api/nodes/${id}/haproxy/reload`)
                  }}
                >
                  <ReloadIcon />
                  Перезагрузить HAProxy
                </button>
                <button
                  className="actions-menu-item ok"
                  type="button"
                  role="menuitem"
                  disabled={busy || st !== 'online'}
                  onClick={() => {
                    setActionsOpen(false)
                    void onRestartHaproxy()
                  }}
                >
                  <RestartIcon />
                  Рестарт
                </button>
                <button
                  className="actions-menu-item"
                  type="button"
                  role="menuitem"
                  onClick={() => void toggleTrafficLog()}
                >
                  <TrafficLogIcon />
                  {node?.traffic_log ? 'Выключить подсчёт трафика' : 'Включить подсчёт трафика'}
                </button>
                <div className="actions-menu-sep" />
                <button
                  className="actions-menu-item danger"
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    setActionsOpen(false)
                    void onDeleteNode()
                  }}
                >
                  <TrashIcon />
                  Удалить ноду
                </button>
              </div>
            )}
          </div>
        </div>
      </header>

      {editOpen && node && (
        <section className="panel" style={{ marginBottom: '1rem' }}>
          <h2>Адрес агента</h2>
          <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
            IP/домен и порт, по которым панель ходит к агенту (не клиентский 8443). Токен не
            меняется.
          </p>
          <form className="stack" onSubmit={onSaveAddress}>
            <div className="row" style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
              <div className="field" style={{ flex: '1 1 12rem' }}>
                <label htmlFor="edit-host">IP или домен</label>
                <input
                  id="edit-host"
                  className="mono"
                  value={editHost}
                  onChange={(e) => setEditHost(e.target.value)}
                  required
                  placeholder="94.241.142.98"
                />
              </div>
              <div className="field" style={{ flex: '0 0 8rem' }}>
                <label htmlFor="edit-port">Порт</label>
                <input
                  id="edit-port"
                  className="mono"
                  value={editPort}
                  onChange={(e) => setEditPort(e.target.value)}
                  required
                  inputMode="numeric"
                />
              </div>
            </div>
            <div className="row">
              <button className="btn btn-primary" type="submit" disabled={editBusy}>
                {editBusy ? 'Сохранение…' : 'Сохранить'}
              </button>
              <button
                className="btn btn-ghost"
                type="button"
                disabled={editBusy}
                onClick={() => setEditOpen(false)}
              >
                Отмена
              </button>
            </div>
          </form>
        </section>
      )}

      {node && (
        <section className="panel" style={{ marginBottom: '1rem' }}>
          <div className="panel-head">
            <h2>1. Входы (порты)</h2>
            <div className="panel-head-aside">
              <button
                className="btn btn-sm btn-primary"
                type="button"
                disabled={entrancesBusy || st !== 'online'}
                onClick={() => {
                  setError('')
                  setEntranceDraft({ port: '' })
                  setShowAddEntrance(true)
                }}
              >
                + Порт
              </button>
            </div>
          </div>
          <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
            Только порты, которые слушает нода. Цели Remnawave добавляются ниже — отдельно на
            каждый порт.
          </p>
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>Порт</th>
                  <th>Целей</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {nodeEntrances.map((ent) => {
                  const n = serversForEntrance(ent).length
                  return (
                    <tr key={ent.port}>
                      <td className="mono">:{ent.port}</td>
                      <td className="muted">{n}</td>
                      <td>
                        <button
                          className="btn btn-sm btn-ghost"
                          type="button"
                          disabled={entrancesBusy || nodeEntrances.length <= 1}
                          onClick={() => void onDeleteEntrance(ent.port)}
                        >
                          Удалить порт
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {node && (
        <section className="panel" style={{ marginBottom: '1rem' }}>
          <h2>2. Бэкенды (серверы на каждый вход)</h2>
          <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
            На каждый порт — свои серверы. Удаление сервера с :443 не трогает :8443.
          </p>
          <div className="stack" style={{ gap: '1rem' }}>
            {nodeEntrances.map((ent) => {
              const list = serversForEntrance(ent)
              return (
                <div
                  key={ent.port}
                  style={{
                    border: '1px solid var(--border, #333)',
                    borderRadius: 8,
                    padding: '0.75rem 1rem',
                  }}
                >
                  <div
                    className="row"
                    style={{
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginBottom: '0.5rem',
                      flexWrap: 'wrap',
                      gap: 8,
                    }}
                  >
                    <strong>
                      Порт <span className="mono">:{ent.port}</span>
                    </strong>
                    <button
                      className="btn btn-sm btn-primary"
                      type="button"
                      disabled={busy || st !== 'online'}
                      onClick={() => openAddServer(ent.port)}
                    >
                      + Сервер на :{ent.port}
                    </button>
                  </div>
                  {list.length === 0 ? (
                    <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
                      Пока пусто — добавьте IP/домен Remnawave-ноды для этого порта.
                    </p>
                  ) : (
                    <div className="table-wrap">
                      <table className="table">
                        <thead>
                          <tr>
                            <th>Имя</th>
                            <th>Адрес</th>
                            <th>Порт цели</th>
                            <th>Вес</th>
                            <th>IP клиента</th>
                            <th>Проверка</th>
                            <th>Статус</th>
                            <th />
                          </tr>
                        </thead>
                        <tbody>
                          {list.map((b) => {
                            const k = remnaKey(b.backend, b.name)
                            const rf = remnaFormFor(b.backend, b.name)
                            const linked = Boolean(remnaLinked[k])
                            const stRow = remnaStats[k]
                            const busyRemna = remnaBusyKey === k
                            const panelId = stRow?.remna_panel_id || rf.panelId
                            const panelName =
                              remnaPanels.find((p) => p.id === panelId)?.name ||
                              panelId ||
                              'Remna'
                            return (
                              <Fragment key={k}>
                                <tr>
                                  <td className="mono">{b.name}</td>
                                  <td className="mono">{b.address}</td>
                                  <td className="mono">{b.port}</td>
                                  <td className="mono">{b.weight ?? '—'}</td>
                                  <td>
                                    <select
                                      aria-label={`IP клиента ${b.name}`}
                                      value={b.client_ip || 'off'}
                                      disabled={busy || st !== 'online'}
                                      onChange={(e) => void onClientIP(b, e.target.value)}
                                    >
                                      <option value="off">выкл</option>
                                      <option value="proxy-ng">proxy-ng</option>
                                      <option value="proxy-vl">proxy-vl</option>
                                    </select>
                                  </td>
                                  <td>
                                    <select
                                      aria-label={`Проверка ${b.name}`}
                                      value={b.check || 'off'}
                                      disabled={busy || st !== 'online'}
                                      onChange={(e) => void onCheck(b, e.target.value)}
                                    >
                                      <option value="off">выкл</option>
                                      <option value="tcp">tcp</option>
                                      <option value="tls">tls</option>
                                    </select>
                                  </td>
                                  <td>{b.status ?? '—'}</td>
                                  <td>
                                    <button
                                      className="btn btn-sm btn-danger"
                                      type="button"
                                      disabled={busy}
                                      onClick={() => void onDeleteBackend(b.backend, b.name)}
                                    >
                                      Удалить
                                    </button>
                                  </td>
                                </tr>
                                <tr className="backend-remna-row">
                                  <td colSpan={8}>
                                    <div className="backend-remna">
                                      {!linked ? (
                                        <div className="backend-remna-bind">
                                          {proxyTraffic(b.backend)}
                                          <select
                                            value={rf.panelId}
                                            aria-label="Панель Remnawave"
                                            onChange={(e) =>
                                              setRemnaForm(b.backend, b.name, {
                                                panelId: e.target.value,
                                              })
                                            }
                                          >
                                            <option value="">— Remnawave —</option>
                                            {remnaPanels.map((p) => (
                                              <option key={p.id} value={p.id}>
                                                {p.name}
                                              </option>
                                            ))}
                                          </select>
                                          <input
                                            value={rf.remnaAddress}
                                            placeholder="Адрес ноды (IP или домен)"
                                            aria-label="Адрес ноды"
                                            title="Как в Remnawave → Address: IP или домен"
                                            onChange={(e) =>
                                              setRemnaForm(b.backend, b.name, {
                                                remnaAddress: e.target.value,
                                              })
                                            }
                                          />
                                          <button
                                            className="btn btn-sm"
                                            type="button"
                                            disabled={busyRemna}
                                            onClick={() => void onSaveRemna(b.backend, b.name)}
                                          >
                                            {busyRemna ? '…' : 'Сохранить'}
                                          </button>
                                        </div>
                                      ) : (
                                        <div className="backend-remna-strip">
                                          <span
                                            className="backend-remna-panel"
                                            title={rf.remnaAddress || undefined}
                                          >
                                            {panelName}
                                          </span>
                                          {proxyTraffic(b.backend)}
                                          {!stRow ? (
                                            <span className="muted">…</span>
                                          ) : stRow.error || stRow.missing ? (
                                            <span className="backend-remna-err">
                                              {stRow.missing
                                                ? 'нода не найдена в Remna'
                                                : stRow.error || 'ошибка'}
                                            </span>
                                          ) : (
                                            <>
                                              <span
                                                className={`status-inline ${stRow.online ? 'online' : 'offline'}`}
                                              >
                                                <span className="status-inline-dot" aria-hidden />
                                                {stRow.online ? 'online' : 'offline'}
                                              </span>
                                              <span
                                                className="backend-remna-metric mono"
                                                title="Users online"
                                              >
                                                <span className="label">users</span>
                                                {stRow.users_online != null &&
                                                Number.isFinite(stRow.users_online)
                                                  ? stRow.users_online
                                                  : '—'}
                                              </span>
                                              <span className="backend-remna-metric mono" title="RAM">
                                                <span className="label">ram</span>
                                                {stRow.ram_percent != null &&
                                                Number.isFinite(stRow.ram_percent)
                                                  ? `${Math.round(stRow.ram_percent)}%`
                                                  : '—'}
                                              </span>
                                              <span
                                                className="backend-remna-metric mono"
                                                title={
                                                  stRow.cpu_count
                                                    ? `Load average (${stRow.cpu_count} cores)`
                                                    : 'Load average'
                                                }
                                              >
                                                <span className="label">load</span>
                                                {stRow.load_avg && stRow.load_avg.length >= 3
                                                  ? stRow.load_avg
                                                      .slice(0, 3)
                                                      .map((n) => Number(n).toFixed(2))
                                                      .join(' ')
                                                  : '—'}
                                              </span>
                                              <span className="node-live-net down" title="RX ↓">
                                                <span className="node-live-arrow" aria-hidden>
                                                  ↓
                                                </span>
                                                {formatBitrateShort(stRow.down_bps)}
                                              </span>
                                              <span className="node-live-net up" title="TX ↑">
                                                <span className="node-live-arrow" aria-hidden>
                                                  ↑
                                                </span>
                                                {formatBitrateShort(stRow.up_bps)}
                                              </span>
                                              <span
                                                className="backend-remna-metric mono"
                                                title="Traffic used / limit"
                                              >
                                                <span className="label">traf</span>
                                                {stRow.traffic_used != null
                                                  ? formatBytes(stRow.traffic_used)
                                                  : '—'}
                                                {stRow.traffic_limit != null &&
                                                stRow.traffic_limit > 0
                                                  ? ` / ${formatBytes(stRow.traffic_limit)}`
                                                  : ''}
                                              </span>
                                            </>
                                          )}
                                          <button
                                            className="btn btn-sm btn-ghost"
                                            type="button"
                                            disabled={busyRemna}
                                            onClick={() => void onClearRemna(b.backend, b.name)}
                                          >
                                            {busyRemna ? '…' : 'Сбросить'}
                                          </button>
                                        </div>
                                      )}
                                    </div>
                                  </td>
                                </tr>
                              </Fragment>
                            )
                          })}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
          {orphanServers.length > 0 && (
            <div
              style={{
                marginTop: '1rem',
                border: '1px solid var(--border, #333)',
                borderRadius: 8,
                padding: '0.75rem 1rem',
              }}
            >
              <strong>Серверы вне входов</strong>
              <p className="muted" style={{ margin: '0.35rem 0 0.5rem', fontSize: '0.85rem' }}>
                Есть на ноде, но не привязаны к текущим портам (старый backend).
              </p>
              <div className="table-wrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Backend</th>
                      <th>Имя</th>
                      <th>Адрес</th>
                      <th>Порт</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {orphanServers.map((b) => (
                      <tr key={`${b.backend}-${b.name}`}>
                        <td className="mono">{b.backend}</td>
                        <td className="mono">{b.name}</td>
                        <td className="mono">{b.address}</td>
                        <td className="mono">{b.port}</td>
                        <td>
                          <button
                            className="btn btn-sm btn-danger"
                            type="button"
                            disabled={busy}
                            onClick={() => void onDeleteBackend(b.backend, b.name)}
                          >
                            Удалить
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
          {st !== 'online' && (
            <p className="muted" style={{ margin: '0.75rem 0 0', fontSize: '0.85rem' }}>
              Сначала проверьте связь с нодой.
            </p>
          )}
        </section>
      )}

      {node && (
        <section className="panel" style={{ marginBottom: '1rem' }}>
          <h2>Защита HAProxy</h2>
          <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
            По умолчанию включены проверка TLS ClientHello (5с) и лимиты на IP. RU allowlist и
            SYNPROXY пока только сохраняются в профиле — применение на хост будет в следующем
            этапе.
          </p>
          <form className="stack" onSubmit={(e) => void onSaveProtect(e)}>
            <label className="row" style={{ alignItems: 'center', gap: 8, cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={protectForm.client_hello}
                onChange={(e) =>
                  setProtectForm((p) => ({ ...p, client_hello: e.target.checked }))
                }
              />
              <span>
                Требовать TLS ClientHello за{' '}
                <input
                  className="mono"
                  style={{ width: '3.5rem' }}
                  type="number"
                  min={1}
                  max={60}
                  value={protectForm.client_hello_delay_sec}
                  onChange={(e) =>
                    setProtectForm((p) => ({
                      ...p,
                      client_hello_delay_sec: Number(e.target.value) || 5,
                    }))
                  }
                />{' '}
                с (режет пустой TCP)
              </span>
            </label>
            <label className="row" style={{ alignItems: 'center', gap: 8, cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={protectForm.rate_limit}
                onChange={(e) =>
                  setProtectForm((p) => ({ ...p, rate_limit: e.target.checked }))
                }
              />
              <span>Лимиты на IP (stick-table)</span>
            </label>
            {protectForm.rate_limit && (
              <div className="row" style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
                <div className="field" style={{ flex: '0 0 8rem' }}>
                  <label htmlFor="max-conn">max conn/IP</label>
                  <input
                    id="max-conn"
                    className="mono"
                    type="number"
                    min={1}
                    value={protectForm.max_conn_per_ip}
                    onChange={(e) =>
                      setProtectForm((p) => ({
                        ...p,
                        max_conn_per_ip: Number(e.target.value) || 100,
                      }))
                    }
                  />
                </div>
                <div className="field" style={{ flex: '0 0 8rem' }}>
                  <label htmlFor="max-rate">новых / окно</label>
                  <input
                    id="max-rate"
                    className="mono"
                    type="number"
                    min={1}
                    value={protectForm.max_rate_per_ip}
                    onChange={(e) =>
                      setProtectForm((p) => ({
                        ...p,
                        max_rate_per_ip: Number(e.target.value) || 60,
                      }))
                    }
                  />
                </div>
                <div className="field" style={{ flex: '0 0 7rem' }}>
                  <label htmlFor="rate-win">окно, с</label>
                  <input
                    id="rate-win"
                    className="mono"
                    type="number"
                    min={1}
                    value={protectForm.rate_window_sec}
                    onChange={(e) =>
                      setProtectForm((p) => ({
                        ...p,
                        rate_window_sec: Number(e.target.value) || 10,
                      }))
                    }
                  />
                </div>
              </div>
            )}
            <label
              className="row"
              style={{ alignItems: 'center', gap: 8, opacity: 0.55, cursor: 'not-allowed' }}
              title="Скоро: nftables на хосте"
            >
              <input type="checkbox" checked={false} disabled />
              <span>Только RU (ASN) — скоро</span>
            </label>
            <label
              className="row"
              style={{ alignItems: 'center', gap: 8, opacity: 0.55, cursor: 'not-allowed' }}
              title="Скоро: nftables на хосте"
            >
              <input type="checkbox" checked={false} disabled />
              <span>SYNPROXY — скоро</span>
            </label>
            <div className="row">
              <button
                className="btn btn-primary"
                type="submit"
                disabled={protectBusy || st !== 'online'}
              >
                {protectBusy ? 'Применение…' : 'Применить защиту'}
              </button>
              {st !== 'online' && (
                <span className="muted" style={{ fontSize: '0.85rem' }}>
                  Сначала проверьте связь с нодой
                </span>
              )}
            </div>
          </form>
        </section>
      )}

      {toast && (
        <div className={`toast toast-${toast.kind}`} role="status">
          {toast.text}
        </div>
      )}

      {st !== 'online' && node && (
        <div className="panel install-hint">
          <p className="muted" style={{ margin: 0 }}>
            Нода не подключена. Запустите docker compose на сервере, затем
            проверьте связь. Если файлы нужны снова — откройте установку.
          </p>
          <button className="btn btn-sm" type="button" onClick={() => void openInstall()}>
            Показать docker compose
          </button>
        </div>
      )}

      {error && (
        <div className="connect-result fail" style={{ marginBottom: '1rem' }}>
          <strong>{error}</strong>
        </div>
      )}

      <div className="stack" style={{ gap: '1rem' }}>
        <section className="panel">
          <div className="panel-head">
            <h2>Система</h2>
            <div className="panel-head-aside">
              <StatusBadge status={st} />
              {st === 'online' && node?.live?.agent_version ? (
                <span
                  className="mono muted"
                  title="Контейнер hapanel-agent (образ azamatbash/hanode)"
                >
                  агент {node.live.agent_version}
                </span>
              ) : null}
              <button
                className="btn btn-primary btn-connect"
                type="button"
                disabled={connecting || busy || !node}
                onClick={() => void onConnect()}
              >
                {connecting ? (
                  <span className="btn-spinner-label">
                    <span className="spinner" aria-hidden />
                    Проверка…
                  </span>
                ) : (
                  'Проверить связь'
                )}
              </button>
            </div>
          </div>
          {st === 'online' ? (
            <>
              <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
                Метрики хоста ноды. Графики обновляются каждые {POLL_MS / 1000} с, пока страница
                открыта.
              </p>
              <div className="stats-grid cols-5" style={{ marginBottom: '0.75rem' }}>
                <div className="stat">
                  <div className="label">CPU</div>
                  <div className="value">
                    {system ? `${system.cpu_percent.toFixed(1)}%` : '—'}
                  </div>
                </div>
                <div className="stat">
                  <div className="label">RAM</div>
                  <div className="value">
                    {system ? `${system.mem_percent.toFixed(1)}%` : '—'}
                  </div>
                  <div className="sub">
                    {system
                      ? `${formatBytes(system.mem_used_bytes)} / ${formatBytes(system.mem_total_bytes)}`
                      : ''}
                  </div>
                </div>
                <div className="stat">
                  <div className="label">Load (1/5/15)</div>
                  <div className="value" style={{ fontSize: '1rem' }}>
                    {system?.load_avg && system.load_avg.length >= 3
                      ? system.load_avg.map((n) => n.toFixed(2)).join(' / ')
                      : '—'}
                  </div>
                </div>
                <div className="stat">
                  <div className="label">Аптайм</div>
                  <div className="value" style={{ fontSize: '1rem' }}>
                    {formatUptime(system?.uptime_seconds)}
                  </div>
                </div>
                {system?.disk_total_bytes ? (
                  <div className="stat">
                    <div className="label">Диск /</div>
                    <div className="value">{(system.disk_percent ?? 0).toFixed(1)}%</div>
                    <div className="sub">
                      {formatBytes(system.disk_used_bytes)} / {formatBytes(system.disk_total_bytes)}
                      {system.disk_inodes_total
                        ? ` · inode ${system.disk_inodes_used ?? 0}/${system.disk_inodes_total}`
                        : ''}
                    </div>
                  </div>
                ) : null}
              </div>
              <div className="stats-grid cols-5" style={{ marginBottom: '0.85rem' }}>
                <div className="stat">
                  <div className="label">Conntrack</div>
                  <div className="value" style={{ color: limitColor(system?.conntrack_percent) }}>
                    {system?.conntrack_max ? `${Math.round(system.conntrack_percent ?? 0)}%` : '—'}
                  </div>
                  {system?.conntrack_max ? (
                    <div className="sub">
                      {`${system.conntrack_count ?? 0} / ${system.conntrack_max}`}
                      {system.conntrack_stat
                        ? ` · fail ${system.conntrack_insert_failed ?? 0}`
                        : ''}
                    </div>
                  ) : (
                    <div className="sub">нет данных</div>
                  )}
                </div>
                <div className="stat">
                  <div className="label">Дескрипторы</div>
                  <div
                    className="value"
                    style={{
                      color: limitColor(
                        system?.fd_max ? ((system.fd_allocated ?? 0) * 100) / system.fd_max : undefined,
                      ),
                    }}
                  >
                    {system?.fd_max ? `${system.fd_allocated ?? 0}` : '—'}
                  </div>
                  <div className="sub">{system?.fd_max ? `из ${system.fd_max}` : 'нет данных'}</div>
                </div>
                <div className="stat">
                  <div className="label">TCP</div>
                  <div className="value">{system?.tcp_stat ? `${system.tcp_inuse ?? 0}` : '—'}</div>
                  <div className="sub">
                    {system?.tcp_stat ? `time-wait ${system.tcp_time_wait ?? 0}` : 'нет данных'}
                  </div>
                </div>
                <div className="stat">
                  <div className="label">Доступно</div>
                  <div className="value" style={{ fontSize: '1rem' }}>
                    {system?.mem_available_bytes != null ? formatBytes(system.mem_available_bytes) : '—'}
                  </div>
                  <div className="sub">
                    {system?.swap_total_bytes
                      ? `swap ${formatBytes(system.swap_used_bytes)} / ${formatBytes(system.swap_total_bytes)}`
                      : system?.scope === 'host'
                        ? 'без swap'
                        : 'нет данных'}
                  </div>
                </div>
                <div className="stat">
                  <div className="label">Давление</div>
                  <div className="value" style={{ fontSize: '1rem' }}>
                    {system?.psi ? fmtAvg(system.psi_memory_some_avg10 ?? 0) : '—'}
                  </div>
                  <div className="sub">
                    {system?.psi
                      ? `cpu ${fmtAvg(system.psi_cpu_some_avg10 ?? 0)} · io ${fmtAvg(system.psi_io_some_avg10 ?? 0)}`
                      : system?.scope === 'host'
                        ? `drop rx ${system.net_rx_drops ?? 0} · tx ${system.net_tx_drops ?? 0}`
                        : 'нет данных'}
                  </div>
                </div>
              </div>
              {haproxyRows.length > 0 ? (
                <div className="table-wrap" style={{ marginBottom: '0.85rem' }}>
                  <table className="table">
                    <thead>
                      <tr>
                        <th>HAProxy</th>
                        <th>Статус</th>
                        <th>Сессии</th>
                        {haproxyShowLimit ? <th>Лимит</th> : null}
                        {haproxyShowQueue ? <th>Очередь</th> : null}
                        <th>Новые/с</th>
                      </tr>
                    </thead>
                    <tbody>
                      {haproxyRows.map((row) => (
                        <tr key={row.key}>
                          <td className="mono">{row.name}</td>
                          <td>{row.status}</td>
                          <td className="mono">{row.sessions}</td>
                          {haproxyShowLimit ? (
                            <td className="mono">{row.limit ? row.limit : '—'}</td>
                          ) : null}
                          {haproxyShowQueue ? (
                            <td className="mono">{row.queue == null ? '—' : row.queue}</td>
                          ) : null}
                          <td className="mono">{row.rate == null ? '—' : row.rate}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : null}
              <div className="charts-grid">
                <SparklineChart
                  label="CPU"
                  color="var(--accent)"
                  points={cpuHist}
                  valueLabel={system ? system.cpu_percent.toFixed(1) : '—'}
                />
                <SparklineChart
                  label="RAM"
                  color="var(--ok)"
                  points={memHist}
                  valueLabel={system ? system.mem_percent.toFixed(1) : '—'}
                />
                <SparklineChart
                  label="Сессии"
                  unit=""
                  color="var(--warn)"
                  points={sessHist}
                  max={Math.max(10, ...sessHist.map((p) => p.v), 1)}
                  valueLabel={
                    facing.active_sessions !== null ? String(facing.active_sessions) : '—'
                  }
                />
                <SparklineChart
                  label="Исходящий TX"
                  unit=""
                  color="var(--accent)"
                  points={downHist}
                  max={Math.max(1, ...downHist.map((p) => p.v), 1)}
                  valueLabel={formatBitrateShort(downBps)}
                />
                <SparklineChart
                  label="Входящий RX"
                  unit=""
                  color="var(--ok)"
                  points={upHist}
                  max={Math.max(1, ...upHist.map((p) => p.v), 1)}
                  valueLabel={formatBitrateShort(upBps)}
                />
              </div>
            </>
          ) : (
            <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
              Метрики появятся после установки связи с нодой.
            </p>
          )}
        </section>

        <section className="panel">
          <div className="panel-head">
            <h2>Трафик</h2>
            <div className="panel-head-aside">
              <label className="traffic-hours-label muted" htmlFor="traffic-hours">
                Период
              </label>
              <select
                id="traffic-hours"
                className="traffic-hours-select"
                value={trafficHours}
                onChange={(e) =>
                  setTrafficHours(
                    Number(e.target.value) as (typeof TRAFFIC_HOURS_OPTIONS)[number],
                  )
                }
              >
                {TRAFFIC_HOURS_OPTIONS.map((h) => (
                  <option key={h} value={h}>
                    {h === 1 ? '1 час' : `${h} ч`}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
            Скорость TX/RX на интерфейсе ноды. История накапливается на панели сутки.
          </p>
          {st === 'online' || trafficPoints.length > 0 ? (
            <TrafficMirrorChart points={trafficPoints} hours={trafficHours} />
          ) : (
            <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
              График появится после установки связи с нодой.
            </p>
          )}
        </section>

        {node?.traffic_log ? (
          <section className="panel">
            <div className="panel-head">
              <h2>Учёт трафика</h2>
              <div className="panel-head-aside traffic-usage-filters">
                <label className="traffic-hours-label muted" htmlFor="usage-from">
                  С
                </label>
                <input
                  id="usage-from"
                  type="date"
                  className="traffic-hours-select"
                  min={usageMin}
                  max={usageMax}
                  value={usageFrom}
                  onChange={(e) => setUsageFrom(e.target.value || usageMin)}
                />
                <label className="traffic-hours-label muted" htmlFor="usage-to">
                  По
                </label>
                <input
                  id="usage-to"
                  type="date"
                  className="traffic-hours-select"
                  min={usageMin}
                  max={usageMax}
                  value={usageTo}
                  onChange={(e) => setUsageTo(e.target.value || usageMax)}
                />
                <div className="traffic-usage-presets">
                  {(
                    [
                      [7, '7 д'],
                      [30, '30 д'],
                      [90, '90 д'],
                    ] as const
                  ).map(([days, label]) => (
                    <button
                      key={days}
                      type="button"
                      className="btn btn-sm"
                      onClick={() => {
                        setUsageFrom(shiftYmd(ymdMsk(), -(days - 1)))
                        setUsageTo(ymdMsk())
                      }}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>
            </div>
            <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
              Сумма входящего и исходящего на NIC ноды за выбранные даты (архив до 90
              дней, с момента включения подсчёта).
            </p>
            {usageLoading && usagePoints.length === 0 ? (
              <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
                Загрузка…
              </p>
            ) : (
              <TrafficUsageChart
                points={usagePoints}
                totalRx={usageTotals.rx}
                totalTx={usageTotals.tx}
              />
            )}
          </section>
        ) : null}
      </div>

      {showAddEntrance && (
        <div className="modal-backdrop" onClick={() => !entrancesBusy && setShowAddEntrance(false)}>
          <div
            className="modal stack"
            onClick={(e) => e.stopPropagation()}
            style={{ width: 'min(28rem, 100%)' }}
          >
            <h3 style={{ margin: 0 }}>Новый вход (порт)</h3>
            <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
              Только порт на ноде. Серверы Remnawave добавите ниже, в блоке «2. Бэкенды».
            </p>
            <form className="stack" onSubmit={(e) => void onAddEntrance(e)}>
              <div className="field">
                <label htmlFor="new-entrance-port">Порт</label>
                <input
                  id="new-entrance-port"
                  className="mono"
                  value={entranceDraft.port}
                  onChange={(e) => setEntranceDraft({ port: e.target.value })}
                  placeholder="443"
                  required
                  inputMode="numeric"
                  disabled={entrancesBusy}
                />
              </div>
              <div className="modal-actions">
                <button
                  className="btn"
                  type="button"
                  disabled={entrancesBusy}
                  onClick={() => setShowAddEntrance(false)}
                >
                  Отменить
                </button>
                <button className="btn btn-primary" type="submit" disabled={entrancesBusy}>
                  {entrancesBusy ? 'Сохранение…' : 'Добавить порт'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {showAddBackend && (
        <div className="modal-backdrop" onClick={closeAddBackend}>
          <div
            className="modal stack"
            onClick={(e) => e.stopPropagation()}
            style={{ width: 'min(32rem, 100%)' }}
          >
            <h3 style={{ margin: 0 }}>
              Сервер на :{form.backend.replace(/^p/, '') || '…'}
            </h3>
            <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
              IP или домен Remnawave-ноды + порт цели. Попадёт только на выбранный вход.
            </p>
            <form className="stack" onSubmit={onAddBackend}>
              {(
                [
                  ['name', 'Имя'],
                  ['address', 'Адрес (IP/домен)'],
                  ['port', 'Порт цели'],
                  ['weight', 'Вес'],
                ] as const
              ).map(([key, label]) => (
                <div className="field" key={key}>
                  <label htmlFor={`add-${key}`}>{label}</label>
                  <input
                    id={`add-${key}`}
                    className={key === 'address' ? 'mono' : undefined}
                    value={form[key]}
                    onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
                    required
                    disabled={busy}
                    placeholder={
                      key === 'address'
                        ? '1.2.3.4'
                        : key === 'port'
                          ? '443'
                          : key === 'weight'
                            ? '100'
                            : undefined
                    }
                  />
                </div>
              ))}
              <div className="field">
                <label htmlFor="add-client-ip">IP клиента</label>
                <select
                  id="add-client-ip"
                  value={form.client_ip}
                  disabled={busy}
                  onChange={(e) => setForm((f) => ({ ...f, client_ip: e.target.value }))}
                >
                  <option value="off">выкл — цель видит адрес этого фронта</option>
                  <option value="proxy-ng">proxy-ng — nginx на цели принимает PROXY protocol</option>
                  <option value="proxy-vl">proxy-vl — Xray на цели принимает PROXY protocol</option>
                </select>
                {form.client_ip !== 'off' && (
                  <p className="muted" style={{ margin: '0.35rem 0 0', fontSize: '0.8rem' }}>
                    {form.client_ip === 'proxy-ng'
                      ? 'Nginx должен слушать этот порт с proxy_protocol. Без этого новые соединения оборвутся.'
                      : 'Xray должен принимать PROXY protocol сам. Настройка nginx сюда не переносится.'}
                  </p>
                )}
              </div>
              <div className="field">
                <label htmlFor="add-check">Проверка</label>
                <select
                  id="add-check"
                  value={form.check}
                  disabled={busy}
                  onChange={(e) => setForm((f) => ({ ...f, check: e.target.value }))}
                >
                  <option value="off">выкл</option>
                  <option value="tcp">tcp — открыт ли порт</option>
                  <option value="tls">tls — рукопожатие, verify none</option>
                </select>
              </div>
              <div className="field">
                <label htmlFor="add-balance">Балансировка</label>
                <select
                  id="add-balance"
                  value={form.balance}
                  disabled={busy}
                  onChange={(e) => setForm((f) => ({ ...f, balance: e.target.value }))}
                >
                  <option value="leastconn">leastconn</option>
                  <option value="roundrobin">roundrobin</option>
                  <option value="source">source</option>
                  <option value="first">first</option>
                  <option value="random">random</option>
                </select>
              </div>
              <div className="modal-actions">
                <button className="btn" type="button" disabled={busy} onClick={closeAddBackend}>
                  Отменить
                </button>
                <button className="btn btn-primary" type="submit" disabled={busy}>
                  {busy ? 'Сохранение…' : 'Добавить сервер'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {showInstall && installBundle && (
        <div className="modal-backdrop" onClick={() => setShowInstall(false)}>
          <div
            className="modal modal-wide stack"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <h3 style={{ margin: 0 }}>Установка ноды</h3>
              <button className="btn btn-sm" type="button" onClick={() => setShowInstall(false)}>
                Закрыть
              </button>
            </div>
            <p className="muted" style={{ margin: 0 }}>
              URL: <span className="mono">{installBundle.url}</span>
            </p>
            <div className="field">
              <label>Файл</label>
              <select
                value={installFileKey}
                onChange={(e) => setInstallFileKey(e.target.value)}
              >
                {Object.keys(installBundle.files).map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
              </select>
            </div>
            <pre className="pre pre-tall">
              {installBundle.files[installFileKey] ?? ''}
            </pre>
            <div className="row">
              <button
                className="btn btn-sm"
                type="button"
                onClick={() =>
                  void copyText(
                    installFileKey,
                    installBundle.files[installFileKey] ?? '',
                  )
                }
              >
                {copied === installFileKey ? 'Скопировано' : `Копировать ${installFileKey}`}
              </button>
              <button
                className="btn btn-sm"
                type="button"
                onClick={() =>
                  downloadTextFile(
                    installFileKey,
                    installBundle.files[installFileKey] ?? '',
                  )
                }
              >
                Скачать файл
              </button>
              <button
                className="btn btn-sm"
                type="button"
                onClick={() => void copyText('commands', installBundle.commands)}
              >
                {copied === 'commands' ? 'Скопировано' : 'Копировать команды'}
              </button>
              <button
                className="btn btn-primary"
                type="button"
                disabled={connecting}
                onClick={() => void onConnect()}
              >
                {connecting ? 'Проверка…' : 'Проверить связь'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
