import { FormEvent, Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  api,
  BackendRemnaLink,
  BackendServer,
  flattenBackends,
  formatBitrateShort,
  formatBytes,
  formatUptime,
  Node,
  Provider,
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
import { ProviderBadge } from './NodesPage'

const HISTORY_MAX = 720
const POLL_MS = 5000
const TRAFFIC_HOURS_OPTIONS = [1, 2, 3, 6, 12, 24] as const
/** Max samples kept live in the browser for the longest window (24h @ 5s). */
const TRAFFIC_HISTORY_MAX = (24 * 60 * 60) / 5

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
  const [downBps, setDownBps] = useState<number | null>(null)
  const [upBps, setUpBps] = useState<number | null>(null)
  const [backends, setBackends] = useState<BackendServer[]>([])
  const histResetRef = useRef(id)
  const trafficRef = useRef<{ t: number; inn: number; out: number } | null>(null)
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
  const [actionsOpen, setActionsOpen] = useState(false)
  const [showAddBackend, setShowAddBackend] = useState(false)
  const actionsRef = useRef<HTMLDivElement | null>(null)

  const [form, setForm] = useState({
    backend: 'app',
    name: '',
    address: '',
    port: '8443',
    weight: '100',
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
      const res = await api<{ stats: RemnaBackendStat[] }>(
        `/api/nodes/${id}/backends/remna-stats`,
      )
      const list = Array.isArray(res.stats) ? res.stats : []
      const map: Record<string, RemnaBackendStat> = {}
      for (const s of list) {
        map[remnaKey(s.backend, s.name)] = s
      }
      setRemnaStats(map)
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
      const list = await api<{ nodes: Node[] }>('/api/nodes')
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
      if (found.status !== 'online') {
        setStats(null)
        setSystem(null)
        setBackends([])
        setUpBps(null)
        setDownBps(null)
        return
      }
      const [statsRes, backendsRes, systemRes] = await Promise.all([
        api<StatsSummary>(`/api/nodes/${id}/stats`),
        api<unknown>(`/api/nodes/${id}/backends`),
        api<SystemMetrics>(`/api/nodes/${id}/system`).catch(() => null),
      ])
      setStats(statsRes)
      const flat = flattenBackends(backendsRes)
      setBackends(flat)
      const facingNow = userFacingStats(statsRes)
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
      putNodeCache(id!, {
        backends: flat,
        sessions:
          facingNow.active_sessions !== null ? String(facingNow.active_sessions) : '—',
        metrics,
      })
      if (facingNow.active_sessions !== null) {
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
    void load()
  }, [load])

  useEffect(() => {
    void loadRemnaPanels()
    void loadProviders()
    void loadRemnaLinks()
  }, [loadRemnaPanels, loadProviders, loadRemnaLinks])

  // Live poll while detail page is open and node is online.
  useEffect(() => {
    if (!node || node.status !== 'online') return
    const timer = setInterval(() => {
      void (async () => {
        try {
          const [statsRes, systemRes] = await Promise.all([
            api<StatsSummary>(`/api/nodes/${id}/stats`),
            api<SystemMetrics>(`/api/nodes/${id}/system`),
          ])
          setStats(statsRes)
          setSystem(systemRes)
          const rates = applyTraffic(systemRes)
          setCpuHist((h) => pushPoint(h, Number(systemRes.cpu_percent) || 0))
          setMemHist((h) => pushPoint(h, Number(systemRes.mem_percent) || 0))
          const facingNow = userFacingStats(statsRes)
          putNodeCache(id!, {
            sessions:
              facingNow.active_sessions !== null
                ? String(facingNow.active_sessions)
                : undefined,
            metrics: {
              cpu: Number(systemRes.cpu_percent) || 0,
              loadAvg: Array.isArray(systemRes.load_avg)
                ? systemRes.load_avg.map(Number)
                : undefined,
              downBps: rates.downBps,
              upBps: rates.upBps,
            },
          })
          if (facingNow.active_sessions !== null) {
            setSessHist((h) => pushPoint(h, facingNow.active_sessions ?? 0))
          }
        } catch {
          /* ignore transient poll errors */
        }
      })()
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [id, node?.status, applyTraffic])

  // Remna stats poll (~5s) when backends are present.
  useEffect(() => {
    if (!id || backends.length === 0) return
    void loadRemnaStats()
    const timer = setInterval(() => {
      void loadRemnaStats()
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [id, backends.length, loadRemnaStats])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), 4500)
    return () => clearTimeout(t)
  }, [toast])

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

  const title = useMemo(() => node?.name ?? 'Нода', [node])
  const st = node?.status || 'unknown'

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
        }),
      })
      setForm((f) => ({ ...f, name: '', address: '' }))
      setShowAddBackend(false)
      setToast({ kind: 'ok', text: 'Бэкенд добавлен' })
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось добавить бэкенд')
    } finally {
      setBusy(false)
    }
  }

  function closeAddBackend() {
    if (busy) return
    setShowAddBackend(false)
  }

  async function onDeleteBackend(backend: string, name: string) {
    if (!confirm(`Удалить ${backend}/${name}?`)) return
    setBusy(true)
    setError('')
    try {
      await api(
        `/api/nodes/${id}/backends/${encodeURIComponent(backend)}/${encodeURIComponent(name)}`,
        { method: 'DELETE' },
      )
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить')
    } finally {
      setBusy(false)
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
      navigate('/')
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
          <Link to="/" className="btn btn-sm btn-ghost back-btn">
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
              <div className="stats-grid" style={{ marginBottom: '0.85rem' }}>
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
                  <div className="muted" style={{ fontSize: '0.78rem', marginTop: '0.2rem' }}>
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
                    <div className="muted" style={{ fontSize: '0.78rem', marginTop: '0.2rem' }}>
                      {formatBytes(system.disk_used_bytes)} / {formatBytes(system.disk_total_bytes)}
                    </div>
                  </div>
                ) : null}
              </div>
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
            <h2>Бэкенды приложений</h2>
            <div className="panel-head-aside">
              <button
                className="btn btn-sm btn-primary"
                type="button"
                disabled={busy || connecting || st !== 'online'}
                onClick={() => {
                  setError('')
                  setShowAddBackend(true)
                }}
              >
                Добавить бэкенд
              </button>
            </div>
          </div>
          <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
            Только пользовательские серверы (app и добавленные вами). Служебные
            hap_agent / acme скрыты.
          </p>
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>Бэкенд</th>
                  <th>Имя</th>
                  <th>Адрес</th>
                  <th>Порт</th>
                  <th>Вес</th>
                  <th>Статус</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {backends.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="muted">
                      {st === 'online'
                        ? 'Пока нет серверов — добавьте через кнопку выше'
                        : 'Нет данных — сначала установите связь'}
                    </td>
                  </tr>
                ) : (
                  backends.map((b) => {
                    const k = remnaKey(b.backend, b.name)
                    const rf = remnaFormFor(b.backend, b.name)
                    const linked = Boolean(remnaLinked[k])
                    const stRow = remnaStats[k]
                    const busyRemna = remnaBusyKey === k
                    const panelId = stRow?.remna_panel_id || rf.panelId
                    const panelName =
                      remnaPanels.find((p) => p.id === panelId)?.name || panelId || 'Remna'
                    return (
                      <Fragment key={k}>
                        <tr>
                          <td className="mono">{b.backend}</td>
                          <td className="mono">{b.name}</td>
                          <td className="mono">{b.address}</td>
                          <td className="mono">{b.port}</td>
                          <td className="mono">{b.weight ?? '—'}</td>
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
                          <td colSpan={7}>
                            <div className="backend-remna">
                              {!linked ? (
                                <div className="backend-remna-bind">
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
                                  <span className="backend-remna-panel" title={rf.remnaAddress || undefined}>
                                    {panelName}
                                  </span>
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
                                      <span className="backend-remna-metric mono" title="Users online">
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
                                        {stRow.traffic_limit != null && stRow.traffic_limit > 0
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
                  })
                )}
              </tbody>
            </table>
          </div>
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
            Скорость TX/RX на интерфейсе ноды. История накапливается на панели.
          </p>
          {st === 'online' || trafficPoints.length > 0 ? (
            <TrafficMirrorChart points={trafficPoints} hours={trafficHours} />
          ) : (
            <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
              График появится после установки связи с нодой.
            </p>
          )}
        </section>
      </div>

      {showAddBackend && (
        <div className="modal-backdrop" onClick={closeAddBackend}>
          <div
            className="modal stack"
            onClick={(e) => e.stopPropagation()}
            style={{ width: 'min(32rem, 100%)' }}
          >
            <h3 style={{ margin: 0 }}>Добавить бэкенд</h3>
            <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
              В «Адрес» пишите IP (или домен) — именно это попадёт в конфиг HAProxy как
              цель <span className="mono">server …</span>. Поле Remnawave ниже по строке
              бэкенда на это не влияет.
            </p>
            <form className="stack" onSubmit={onAddBackend}>
              {(
                [
                  ['backend', 'Бэкенд'],
                  ['name', 'Имя сервера'],
                  ['address', 'Адрес (IP для HAProxy)'],
                  ['port', 'Порт'],
                  ['weight', 'Вес'],
                ] as const
              ).map(([key, label]) => (
                <div className="field" key={key}>
                  <label htmlFor={`add-${key}`}>{label}</label>
                  <input
                    id={`add-${key}`}
                    className={key === 'address' || key === 'backend' ? 'mono' : undefined}
                    value={form[key]}
                    onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
                    required
                    disabled={busy}
                    placeholder={
                      key === 'backend'
                        ? 'app'
                        : key === 'address'
                          ? '1.2.3.4'
                          : key === 'port'
                            ? '8443'
                            : key === 'weight'
                              ? '100'
                              : undefined
                    }
                  />
                </div>
              ))}
              <div className="modal-actions">
                <button
                  className="btn"
                  type="button"
                  disabled={busy}
                  onClick={closeAddBackend}
                >
                  Отменить
                </button>
                <button className="btn btn-primary" type="submit" disabled={busy}>
                  {busy ? 'Сохранение…' : 'Сохранить'}
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
