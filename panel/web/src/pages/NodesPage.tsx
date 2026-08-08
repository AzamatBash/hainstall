import { FormEvent, Fragment, useCallback, useEffect, useRef, useState, type DragEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  api,
  BackendServer,
  formatBitrateShort,
  Node,
  Provider,
  RemnaPanel,
  setToken,
  statusLabel,
  translateError,
} from '../api'
import { copyToClipboard, downloadTextFile } from '../clipboard'
import CountryPicker from '../components/CountryPicker'
import TrafficMirrorChart, { type TrafficPoint } from '../components/TrafficMirrorChart'
import {
  getNodesCacheMap,
  NodeLiveMetrics,
  putNodesCache,
  removeNodeCache,
} from '../nodeCache'
import { withBase } from '../basePath'

const METRICS_POLL_MS = 5000

/** Resolve a domain or URL to a favicon image src (Google s2). */
export function providerFaviconSrc(favicon: string): string {
  const s = (favicon || '').trim()
  if (!s) return ''
  try {
    const u = new URL(s.includes('://') ? s : `https://${s}`)
    return `https://www.google.com/s2/favicons?domain=${encodeURIComponent(u.hostname)}&sz=32`
  } catch {
    return ''
  }
}

export function ProviderBadge({
  name,
  favicon,
  loginUrl,
  accountLogin,
}: {
  name: string
  favicon?: string
  loginUrl?: string
  accountLogin?: string
}) {
  const icon = providerFaviconSrc(favicon || '')
  const label = name.trim()
  if (!label) return null
  const account = (accountLogin || '').trim()
  const href = (loginUrl || '').trim()
  const badgeInner = (
    <>
      {icon ? (
        <img className="provider-badge-icon" src={icon} alt="" width={14} height={14} />
      ) : null}
      <span className="provider-badge-name">{label}</span>
    </>
  )
  const badge = href ? (
    <a
      className="provider-badge"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={href}
      onClick={(e) => e.stopPropagation()}
    >
      {badgeInner}
    </a>
  ) : (
    <span className="provider-badge">{badgeInner}</span>
  )
  return (
    <span className="provider-badge-wrap">
      {badge}
      {account ? (
        <span
          className="provider-account-hint"
          tabIndex={0}
          aria-label={`Аккаунт: ${account}`}
          onClick={(e) => e.stopPropagation()}
        >
          <span className="provider-account-bang" aria-hidden>
            !
          </span>
          <span className="provider-account-tip" role="tooltip">
            <span className="provider-account-tip-label">Аккаунт</span>
            <span className="provider-account-tip-value">{account}</span>
          </span>
        </span>
      ) : null}
    </span>
  )
}

function StatusBadge({ status, large }: { status: string; large?: boolean }) {
  const s = status || 'unknown'
  return (
    <span className={`badge badge-status ${s}${large ? ' badge-lg' : ''}`}>
      {statusLabel(s)}
    </span>
  )
}

/** Compact variant A: dot + muted text, no pill. */
function StatusInline({ status }: { status: string }) {
  const s = status || 'unknown'
  return (
    <span className={`status-inline ${s}`} title={statusLabel(s)}>
      <span className="status-inline-dot" aria-hidden />
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

function nodeMatchesSearch(
  n: Node,
  query: string,
  backends: BackendServer[],
): boolean {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  if (n.name.toLowerCase().includes(needle)) return true
  const host = nodeHost(n.url).toLowerCase()
  if (host.includes(needle)) return true
  if ((n.url || '').toLowerCase().includes(needle)) return true
  for (const addr of n.remna_addresses ?? []) {
    if (addr.toLowerCase().includes(needle)) return true
  }
  for (const b of backends) {
    if ((b.name || '').toLowerCase().includes(needle)) return true
    if ((b.backend || '').toLowerCase().includes(needle)) return true
    if ((b.address || '').toLowerCase().includes(needle)) return true
    const port = b.port != null && b.port !== 0 ? String(b.port) : ''
    if (port && `${b.address}:${port}`.toLowerCase().includes(needle)) return true
  }
  return false
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

function SearchIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden>
      <circle cx="11" cy="11" r="6.5" stroke="currentColor" strokeWidth="1.75" />
      <path
        d="M16.5 16.5L21 21"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
    </svg>
  )
}

function cpuBarTone(cpu: number): string {
  if (cpu >= 85) return 'hot'
  if (cpu >= 60) return 'warm'
  return 'ok'
}

function NodeLiveRow({ m }: { m: NodeLiveMetrics }) {
  const cpu = Math.max(0, Math.min(100, Number(m.cpu) || 0))
  const load =
    m.loadAvg && m.loadAvg.length >= 3
      ? m.loadAvg.map((n) => Number(n).toFixed(2)).join(' ')
      : null
  return (
    <div className="node-live">
      <span className={`node-live-cpu ${cpuBarTone(cpu)}`} title="CPU">
        <span className="node-live-bar" aria-hidden>
          <span className="node-live-bar-fill" style={{ width: `${cpu}%` }} />
        </span>
        <span className="mono">{cpu.toFixed(0)}%</span>
      </span>
      {load && (
        <span className="node-live-load mono" title="Load average">
          {load}
        </span>
      )}
      <span className="node-live-net down" title="Отдача (TX)">
        <span className="node-live-arrow" aria-hidden>
          ↓
        </span>
        {formatBitrateShort(m.downBps)}
      </span>
      <span className="node-live-net up" title="Загрузка (RX)">
        <span className="node-live-arrow" aria-hidden>
          ↑
        </span>
        {formatBitrateShort(m.upBps)}
      </span>
    </div>
  )
}

type NodeFilterTab = 'remna' | 'providers' | 'nodes' | 'agent' | string

export default function NodesPage() {
  const navigate = useNavigate()
  const [filterTab, setFilterTab] = useState<NodeFilterTab>('nodes')
  const [panels, setPanels] = useState<RemnaPanel[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [nodes, setNodes] = useState<Node[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [showAdd, setShowAdd] = useState(false)
  const [backendsMap, setBackendsMap] = useState<Record<string, BackendServer[]>>({})
  const [metricsMap, setMetricsMap] = useState<Record<string, NodeLiveMetrics>>({})
  const [trafficMap, setTrafficMap] = useState<Record<string, TrafficPoint[]>>({})
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [renameBusy, setRenameBusy] = useState(false)
  const [copiedId, setCopiedId] = useState('')
  const [dragId, setDragId] = useState<string | null>(null)
  const [dragOverId, setDragOverId] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const menuRef = useRef<HTMLDivElement | null>(null)
  const renameRef = useRef<HTMLInputElement | null>(null)
  const nodesRef = useRef<Node[]>([])
  const orderBeforeDragRef = useRef<string[] | null>(null)
  const dragMovedRef = useRef(false)
  const dragGhostRef = useRef<HTMLElement | null>(null)

  nodesRef.current = nodes

  const applyLiveFromNodes = useCallback((list: Node[]) => {
    const nextBackends: Record<string, BackendServer[]> = {}
    const nextMetrics: Record<string, NodeLiveMetrics> = {}
    const cacheUpdates: Array<{
      id: string
      backends?: BackendServer[]
      sessions?: string
      metrics?: NodeLiveMetrics
    }> = []
    for (const n of list) {
      const online =
        typeof n.remna_online === 'number' && Number.isFinite(n.remna_online)
          ? String(n.remna_online)
          : '0'
      const live = n.live
      if (live && Array.isArray(live.backends)) {
        nextBackends[n.id] = live.backends
      }
      if (live && typeof live.cpu === 'number') {
        const metrics: NodeLiveMetrics = {
          cpu: live.cpu,
          loadAvg: Array.isArray(live.load_avg) ? live.load_avg : undefined,
          downBps: live.down_bps ?? null,
          upBps: live.up_bps ?? null,
        }
        nextMetrics[n.id] = metrics
        cacheUpdates.push({
          id: n.id,
          sessions: online,
          backends: live.backends,
          metrics,
        })
      } else {
        cacheUpdates.push({
          id: n.id,
          sessions: online,
          backends: live?.backends,
        })
      }
    }
    if (Object.keys(nextBackends).length) {
      setBackendsMap((m) => ({ ...m, ...nextBackends }))
    }
    if (Object.keys(nextMetrics).length) {
      setMetricsMap((m) => ({ ...m, ...nextMetrics }))
    }
    if (cacheUpdates.length) putNodesCache(cacheUpdates)
  }, [])

  const loadPanels = useCallback(async () => {
    try {
      const res = await api<{ panels: RemnaPanel[] }>('/api/remna-panels')
      const list = Array.isArray(res.panels) ? res.panels : []
      setPanels(list)
      setFilterTab((cur) => {
        if (cur === 'nodes' || cur === 'remna' || cur === 'providers' || cur === 'agent') return cur
        return list.some((p) => p.id === cur) ? cur : 'nodes'
      })
    } catch {
      /* keep previous panels */
    }
  }, [])

  const loadProviders = useCallback(async () => {
    try {
      const res = await api<{ providers: Provider[] }>('/api/providers')
      setProviders(Array.isArray(res.providers) ? res.providers : [])
    } catch {
      /* keep previous */
    }
  }, [])

  const load = useCallback(async () => {
    setError('')
    try {
      const [res] = await Promise.all([
        api<{ nodes: Node[] }>('/api/nodes'),
        loadPanels(),
        loadProviders(),
      ])
      const list = Array.isArray(res.nodes) ? res.nodes : []
      const cached = getNodesCacheMap(list.map((n) => n.id))
      setBackendsMap({ ...cached.backends })
      setMetricsMap({ ...cached.metrics })
      setNodes(list)
      applyLiveFromNodes(list)
      setLoading(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить ноды')
      setLoading(false)
    }
  }, [applyLiveFromNodes, loadPanels, loadProviders])

  useEffect(() => {
    void load()
  }, [load])

  const loadTraffic = useCallback(async () => {
    try {
      const res = await api<{ nodes: Record<string, TrafficPoint[]> }>('/api/traffic')
      setTrafficMap(res.nodes && typeof res.nodes === 'object' ? res.nodes : {})
    } catch {
      /* optional */
    }
  }, [])

  useEffect(() => {
    void loadTraffic()
  }, [loadTraffic])

  // Soft refresh list snapshots from panel DB (panel poller fills them).
  useEffect(() => {
    const timer = setInterval(() => {
      void (async () => {
        try {
          const res = await api<{ nodes: Node[] }>('/api/nodes')
          const list = Array.isArray(res.nodes) ? res.nodes : []
          setNodes(list)
          applyLiveFromNodes(list)
        } catch {
          /* ignore */
        }
        void loadTraffic()
      })()
    }, METRICS_POLL_MS)
    return () => clearInterval(timer)
  }, [applyLiveFromNodes, loadTraffic])

  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!menuRef.current) return
      if (!menuRef.current.contains(e.target as HTMLElement)) {
        setMenuOpenId(null)
      }
    }
    document.addEventListener('click', onDocClick)
    return () => document.removeEventListener('click', onDocClick)
  }, [])

  useEffect(() => {
    if (renamingId && renameRef.current) {
      renameRef.current.focus()
      renameRef.current.select()
    }
  }, [renamingId])

  const visibleNodes = (
    filterTab === 'nodes' || filterTab === 'remna' || filterTab === 'agent'
      ? nodes
      : nodes.filter((n) => (n.remna_panel_id || '') === filterTab)
  ).filter((n) => nodeMatchesSearch(n, searchQuery, backendsMap[n.id] ?? []))

  const activePanelName =
    filterTab === 'remna'
      ? 'Панели Remnawave'
      : filterTab === 'providers'
        ? 'Провайдеры'
        : filterTab === 'agent'
          ? 'Агент'
          : filterTab === 'nodes'
            ? 'Ноды'
            : panels.find((p) => p.id === filterTab)?.name || 'Ноды'

  async function onCopyHost(id: string, host: string) {
    const ok = await copyToClipboard(host)
    if (ok) {
      setCopiedId(id)
      window.setTimeout(() => setCopiedId((cur) => (cur === id ? '' : cur)), 1500)
    } else {
      setError('Не удалось скопировать')
    }
  }

  function startRename(n: Node) {
    setMenuOpenId(null)
    setRenamingId(n.id)
    setRenameValue(n.name)
  }

  async function submitRename(id: string) {
    const name = renameValue.trim()
    if (!name) {
      setError('Имя не может быть пустым')
      return
    }
    setRenameBusy(true)
    setError('')
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ name }),
      })
      setNodes((list) => list.map((n) => (n.id === id ? { ...n, ...res.node } : n)))
      setRenamingId(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось переименовать')
    } finally {
      setRenameBusy(false)
    }
  }

  async function setCountry(id: string, country: string) {
    setError('')
    try {
      const res = await api<{ node: Node }>(`/api/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ country }),
      })
      setNodes((list) => list.map((n) => (n.id === id ? { ...n, ...res.node } : n)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить страну')
    }
  }

  async function persistOrder(next: Node[]) {
    setNodes(next)
    try {
      const res = await api<{ nodes: Node[] }>('/api/nodes/reorder', {
        method: 'PUT',
        body: JSON.stringify({ ids: next.map((n) => n.id) }),
      })
      if (Array.isArray(res.nodes)) {
        setNodes((cur) => {
          const byId = new Map(res.nodes.map((n) => [n.id, n]))
          return cur.map((n) => {
            const updated = byId.get(n.id)
            return updated ? { ...n, ...updated, status: n.status, last_seen: n.last_seen } : n
          })
        })
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить порядок')
      await load()
    }
  }

  function moveNodeBefore(targetId: string) {
    if (!dragId || dragId === targetId) return
    setNodes((prev) => {
      const from = prev.findIndex((n) => n.id === dragId)
      const to = prev.findIndex((n) => n.id === targetId)
      if (from < 0 || to < 0 || from === to) return prev
      dragMovedRef.current = true
      const next = [...prev]
      const [item] = next.splice(from, 1)
      next.splice(to, 0, item)
      return next
    })
    setDragOverId(targetId)
  }

  function finishDrag() {
    if (!orderBeforeDragRef.current) {
      setDragId(null)
      setDragOverId(null)
      if (dragGhostRef.current) {
        dragGhostRef.current.remove()
        dragGhostRef.current = null
      }
      return
    }
    const before = orderBeforeDragRef.current
    orderBeforeDragRef.current = null
    const after = nodesRef.current.map((n) => n.id)
    const changed = before.join('\0') !== after.join('\0')
    setDragId(null)
    setDragOverId(null)
    if (dragGhostRef.current) {
      dragGhostRef.current.remove()
      dragGhostRef.current = null
    }
    if (changed) {
      void persistOrder(nodesRef.current)
    }
  }

  function startRowDrag(e: DragEvent<HTMLElement>, id: string) {
    e.stopPropagation()
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', id)
    dragMovedRef.current = false
    orderBeforeDragRef.current = nodesRef.current.map((n) => n.id)
    setDragId(id)
    setDragOverId(null)

    const tr = e.currentTarget.closest('tr')
    if (!tr) return
    const rect = tr.getBoundingClientRect()
    const wrap = document.createElement('div')
    wrap.className = 'node-drag-ghost-wrap'
    wrap.style.width = `${rect.width}px`
    const table = document.createElement('table')
    table.className = tr.closest('table')?.className || 'table table-nodes'
    table.style.width = `${rect.width}px`
    const colgroup = tr.closest('table')?.querySelector('colgroup')
    if (colgroup) {
      table.appendChild(colgroup.cloneNode(true))
    } else {
      // Match column widths so the ghost looks like the real plaque.
      const cols = Array.from(tr.children) as HTMLElement[]
      const cg = document.createElement('colgroup')
      for (const cell of cols) {
        const col = document.createElement('col')
        col.style.width = `${cell.getBoundingClientRect().width}px`
        cg.appendChild(col)
      }
      table.appendChild(cg)
    }
    const tbody = document.createElement('tbody')
    const clone = tr.cloneNode(true) as HTMLTableRowElement
    clone.classList.add('node-drag-ghost-row')
    tbody.appendChild(clone)
    table.appendChild(tbody)
    wrap.appendChild(table)
    document.body.appendChild(wrap)
    dragGhostRef.current = wrap
    const offsetX = Math.max(12, e.clientX - rect.left)
    const offsetY = Math.max(12, e.clientY - rect.top)
    e.dataTransfer.setDragImage(wrap, offsetX, offsetY)
    window.setTimeout(() => {
      if (dragGhostRef.current === wrap) {
        wrap.remove()
        dragGhostRef.current = null
      }
    }, 0)
  }

  async function onDelete(id: string, name: string) {
    setMenuOpenId(null)
    if (!confirm(`Удалить ноду «${name}»?`)) return
    try {
      await api(`/api/nodes/${id}`, { method: 'DELETE' })
      removeNodeCache(id)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить')
    }
  }

  async function logout() {
    try {
      await api('/api/auth/logout', { method: 'POST' })
    } catch {
      /* ignore */
    }
    setToken(null)
    window.location.href = withBase('/login')
  }

  return (
    <div className="shell">
      <header className="topbar">
        <div className="brand">
          ha<span>panel</span>
        </div>
        {filterTab !== 'remna' && filterTab !== 'agent' && (
          <label className="nodes-search" title="Поиск по имени, IP ноды, адресу из Remnawave или бэкенда">
            <span className="nodes-search-icon" aria-hidden>
              <SearchIcon />
            </span>
            <input
              type="search"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Имя, IP, адрес из Remnawave…"
              aria-label="Поиск нод"
            />
          </label>
        )}
        <div className="row">
          {filterTab !== 'remna' && filterTab !== 'providers' && filterTab !== 'agent' && (
            <>
              <button className="btn btn-sm" type="button" onClick={() => void load()}>
                Обновить
              </button>
              <button
                className="btn btn-sm btn-primary"
                type="button"
                onClick={() => setShowAdd(true)}
              >
                Добавить ноду
              </button>
            </>
          )}
          <button className="btn btn-sm btn-ghost" type="button" onClick={() => void logout()}>
            Выйти
          </button>
        </div>
      </header>

      <nav className="page-tabs" aria-label="Разделы">
        <button
          type="button"
          className={`page-tab${filterTab === 'remna' ? ' active' : ''}`}
          onClick={() => setFilterTab('remna')}
        >
          Панели Remnawave
        </button>
        <button
          type="button"
          className={`page-tab${filterTab === 'providers' ? ' active' : ''}`}
          onClick={() => setFilterTab('providers')}
        >
          Провайдеры
        </button>
        <button
          type="button"
          className={`page-tab${filterTab === 'nodes' ? ' active' : ''}`}
          onClick={() => setFilterTab('nodes')}
        >
          Ноды
        </button>
        <button
          type="button"
          className={`page-tab${filterTab === 'agent' ? ' active' : ''}`}
          onClick={() => setFilterTab('agent')}
        >
          Агент
        </button>
        {panels.map((p) => (
          <button
            key={p.id}
            type="button"
            className={`page-tab${filterTab === p.id ? ' active' : ''}`}
            onClick={() => setFilterTab(p.id)}
            title={p.base_url}
          >
            {p.name}
          </button>
        ))}
      </nav>

      {filterTab === 'remna' ? (
        <RemnaPanelsSection />
      ) : filterTab === 'providers' ? (
        <ProvidersSection providers={providers} onChange={() => void loadProviders()} />
      ) : filterTab === 'agent' ? (
        <AgentDeployChat onDone={() => void load()} />
      ) : (
      <section className="panel">
        <h2>{activePanelName}</h2>
        {error && <p className="error">{error}</p>}
        {loading ? (
              <p className="muted">Загрузка…</p>
            ) : (nodes?.length ?? 0) === 0 ? (
              <p className="muted">
                Нод пока нет. Добавьте первую — получите готовый docker compose.
              </p>
            ) : visibleNodes.length === 0 ? (
              <p className="muted">
                {searchQuery.trim()
                  ? 'Ничего не найдено — попробуйте другое имя или IP.'
                  : 'В этой вкладке пока нет нод — выберите панель в карточке ноды.'}
              </p>
            ) : (
              <div className="table-wrap">
                <table className="table table-clickable table-nodes">
                  <thead>
                    <tr>
                      <th className="col-drag" aria-label="Order" />
                      <th>Имя</th>
                      <th>Адрес</th>
                      <th>Online</th>
                      <th>Бэкенды</th>
                      <th className="col-actions" />
                    </tr>
                  </thead>
                  <tbody>
                    {visibleNodes.map((n) => {
                      const host = nodeHost(n.url)
                      const backends = backendsMap[n.id] ?? []
                      return (
                        <tr
                          key={n.id}
                          className={`row-click${dragOverId === n.id ? ' drag-over' : ''}${dragId === n.id ? ' dragging' : ''}`}
                          onClick={() => {
                            if (renamingId === n.id) return
                            if (dragMovedRef.current) {
                              dragMovedRef.current = false
                              return
                            }
                            navigate(`/nodes/${n.id}`)
                          }}
                          onDragOver={(e) => {
                            e.preventDefault()
                            e.dataTransfer.dropEffect = 'move'
                            if (dragId && dragId !== n.id) moveNodeBefore(n.id)
                          }}
                          onDrop={(e) => {
                            e.preventDefault()
                            e.stopPropagation()
                            finishDrag()
                          }}
                        >
                          <td
                            className="col-drag"
                            onClick={(e) => e.stopPropagation()}
                            draggable
                            onDragStart={(e) => startRowDrag(e, n.id)}
                            onDragEnd={() => finishDrag()}
                            title="Перетащить"
                          >
                            <span className="drag-handle" aria-hidden>
                              ⠿
                            </span>
                          </td>
                          <td onClick={(e) => renamingId === n.id && e.stopPropagation()}>
                            <div className="name-stack">
                              <div className="name-cell">
                                <div className="name-cell-main">
                                  <span
                                    className="node-remna-panel muted"
                                    title={n.remna_panel_name || undefined}
                                  >
                                    {n.remna_panel_name || '—'}
                                  </span>
                                  <span onClick={(e) => e.stopPropagation()}>
                                    <CountryPicker
                                      value={n.country || ''}
                                      onChange={(code) => void setCountry(n.id, code)}
                                    />
                                  </span>
                                  {renamingId === n.id ? (
                                    <form
                                      className="inline-rename"
                                      onSubmit={(e) => {
                                        e.preventDefault()
                                        void submitRename(n.id)
                                      }}
                                    >
                                      <input
                                        ref={renameRef}
                                        className="inline-rename-input"
                                        value={renameValue}
                                        disabled={renameBusy}
                                        onChange={(e) => setRenameValue(e.target.value)}
                                        onKeyDown={(e) => {
                                          if (e.key === 'Escape') {
                                            e.preventDefault()
                                            setRenamingId(null)
                                          }
                                        }}
                                        onBlur={() => {
                                          window.setTimeout(() => {
                                            if (!renameBusy) setRenamingId(null)
                                          }, 120)
                                        }}
                                      />
                                    </form>
                                  ) : (
                                    <Link
                                      to={`/nodes/${n.id}`}
                                      className="node-name-link"
                                      onClick={(e) => e.stopPropagation()}
                                    >
                                      {n.name}
                                    </Link>
                                  )}
                                </div>
                                <StatusInline status={n.status} />
                              </div>
                              {n.status === 'online' && metricsMap[n.id] && (
                                <NodeLiveRow m={metricsMap[n.id]} />
                              )}
                              {(trafficMap[n.id]?.length ?? 0) > 0 ? (
                                <div
                                  className="node-traffic-wrap"
                                  onClick={(e) => e.stopPropagation()}
                                >
                                  <TrafficMirrorChart
                                    compact
                                    points={trafficMap[n.id] ?? []}
                                  />
                                </div>
                              ) : null}
                            </div>
                          </td>
                          <td onClick={(e) => e.stopPropagation()}>
                            <div className="addr-cell">
                              {n.provider_name ? (
                                <ProviderBadge
                                  name={n.provider_name}
                                  favicon={n.provider_favicon}
                                  loginUrl={n.provider_login_url}
                                  accountLogin={n.provider_account_login}
                                />
                              ) : null}
                              <span className="mono muted">{host}</span>
                              <button
                                type="button"
                                className={`icon-btn${copiedId === n.id ? ' copied' : ''}`}
                                title={copiedId === n.id ? 'Скопировано' : 'Копировать IP'}
                                aria-label="Копировать IP"
                                onClick={() => void onCopyHost(n.id, host)}
                              >
                                <CopyIcon />
                              </button>
                            </div>
                          </td>
                          <td className="mono">{n.remna_online ?? 0}</td>
                          <td
                            className="col-backends"
                            onClick={(e) => e.stopPropagation()}
                          >
                            {backends.length === 0 ? (
                              <span className="muted">—</span>
                            ) : (
                              <div
                                className="backends-scroll"
                                title={backends.map((b) => b.name).join(', ')}
                              >
                                {backends.map((b) => (
                                  <div key={`${b.backend}-${b.name}`} className="backends-scroll-item">
                                    {b.name}
                                  </div>
                                ))}
                              </div>
                            )}
                          </td>
                          <td className="col-actions" onClick={(e) => e.stopPropagation()}>
                            <div
                              className="kebab-wrap"
                              ref={menuOpenId === n.id ? menuRef : undefined}
                            >
                              <button
                                className="btn btn-sm btn-ghost kebab-btn"
                                type="button"
                                aria-label="Действия"
                                onClick={() =>
                                  setMenuOpenId((cur) => (cur === n.id ? null : n.id))
                                }
                              >
                                ⋮
                              </button>
                              {menuOpenId === n.id && (
                                <div className="kebab-menu">
                                  <button
                                    className="kebab-item"
                                    type="button"
                                    onClick={() => startRename(n)}
                                  >
                                    Переименовать
                                  </button>
                                  <button
                                    className="kebab-item danger"
                                    type="button"
                                    onClick={() => void onDelete(n.id, n.name)}
                                  >
                                    Удалить
                                  </button>
                                </div>
                              )}
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

      {showAdd && (
        <AddNodeWizard
          onClose={() => setShowAdd(false)}
          onDone={() => {
            setShowAdd(false)
            void load()
          }}
        />
      )}
    </div>
  )
}

type AgentMsg = {
  at: string
  role: string
  text: string
  step?: string
}

function AgentDeployChat({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [sshUser, setSshUser] = useState('root')
  const [sshPassword, setSshPassword] = useState('')
  const [sshPort, setSshPort] = useState('22')
  const [mgmtPort, setMgmtPort] = useState('47893')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [jobId, setJobId] = useState<string | null>(null)
  const [status, setStatus] = useState('')
  const [nodeId, setNodeId] = useState('')
  const [messages, setMessages] = useState<AgentMsg[]>([])
  const logRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!jobId) return
    let cancelled = false
    const tick = async () => {
      try {
        const res = await api<{
          job: { id: string; status: string; node_id?: string }
          messages: AgentMsg[]
        }>(`/api/agent/jobs/${jobId}`)
        if (cancelled) return
        setStatus(res.job.status)
        setNodeId(res.job.node_id || '')
        setMessages(Array.isArray(res.messages) ? res.messages : [])
        if (res.job.status === 'succeeded' || res.job.status === 'failed') {
          setBusy(false)
          if (res.job.status === 'succeeded') onDone()
          return
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Ошибка опроса задачи')
          setBusy(false)
        }
        return
      }
      window.setTimeout(() => {
        if (!cancelled) void tick()
      }, 1500)
    }
    void tick()
    return () => {
      cancelled = true
    }
  }, [jobId, onDone])

  useEffect(() => {
    const el = logRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages])

  async function onStart(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    setMessages([])
    setStatus('queued')
    setNodeId('')
    setJobId(null)
    try {
      const res = await api<{ job: { id: string; status: string } }>('/api/agent/deploy', {
        method: 'POST',
        body: JSON.stringify({
          name: name.trim(),
          host: host.trim(),
          ssh_user: sshUser.trim() || 'root',
          ssh_password: sshPassword,
          ssh_port: Number(sshPort) || 22,
          mgmt_port: Number(mgmtPort) || 47893,
        }),
      })
      setJobId(res.job.id)
      setStatus(res.job.status)
    } catch (err) {
      setBusy(false)
      setError(err instanceof Error ? err.message : 'Не удалось запустить')
    }
  }

  return (
    <section className="panel agent-panel">
      <h2 style={{ marginTop: 0 }}>Агент развёртывания</h2>
      <p className="muted" style={{ marginTop: 0 }}>
        Сначала playbook сам ставит hanode по SSH. Если шаг падает — подключается Gemini/Groq.
      </p>
      {error && <p className="error">{error}</p>}

      <form className="stack agent-form" onSubmit={onStart}>
        <div className="row" style={{ alignItems: 'flex-end' }}>
          <div className="field" style={{ minWidth: 120 }}>
            <label htmlFor="ag-name">Имя ноды</label>
            <input
              id="ag-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              disabled={busy}
              placeholder="de1"
            />
          </div>
          <div className="field" style={{ minWidth: 140 }}>
            <label htmlFor="ag-host">IP / хост VPS</label>
            <input
              id="ag-host"
              className="mono"
              value={host}
              onChange={(e) => setHost(e.target.value)}
              required
              disabled={busy}
              placeholder="1.2.3.4"
            />
          </div>
          <div className="field" style={{ minWidth: 90 }}>
            <label htmlFor="ag-user">SSH user</label>
            <input
              id="ag-user"
              value={sshUser}
              onChange={(e) => setSshUser(e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="field" style={{ minWidth: 140 }}>
            <label htmlFor="ag-pass">SSH пароль</label>
            <input
              id="ag-pass"
              type="password"
              value={sshPassword}
              onChange={(e) => setSshPassword(e.target.value)}
              required
              disabled={busy}
              autoComplete="off"
            />
          </div>
          <div className="field" style={{ width: 72 }}>
            <label htmlFor="ag-ssh-port">SSH</label>
            <input
              id="ag-ssh-port"
              className="mono"
              value={sshPort}
              onChange={(e) => setSshPort(e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="field" style={{ width: 88 }}>
            <label htmlFor="ag-mgmt">mgmt</label>
            <input
              id="ag-mgmt"
              className="mono"
              value={mgmtPort}
              onChange={(e) => setMgmtPort(e.target.value)}
              disabled={busy}
            />
          </div>
          <button className="btn btn-primary" type="submit" disabled={busy}>
            {busy ? 'Работаю…' : 'Развернуть'}
          </button>
        </div>
      </form>

      {(status || messages.length > 0) && (
        <div className="agent-status muted">
          Статус: <span className="mono">{status || '—'}</span>
          {nodeId ? (
            <>
              {' '}
              · нода{' '}
              <Link to={`/nodes/${nodeId}`} className="mono">
                {nodeId.slice(0, 8)}…
              </Link>
            </>
          ) : null}
        </div>
      )}

      <div className="agent-chat" ref={logRef} aria-live="polite">
        {messages.length === 0 ? (
          <p className="muted" style={{ margin: 0 }}>
            Лог появится после запуска.
          </p>
        ) : (
          messages.map((m, i) => (
            <div key={`${m.at}-${i}`} className={`agent-msg role-${m.role}`}>
              <div className="agent-msg-meta">
                <span className="agent-msg-role">{m.role}</span>
                {m.step ? <span className="muted">{m.step}</span> : null}
              </div>
              <pre className="agent-msg-text">{m.text}</pre>
            </div>
          ))
        )}
      </div>
    </section>
  )
}


function RemnaPanelsSection() {
  const [panels, setPanels] = useState<RemnaPanel[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editName, setEditName] = useState('')
  const [editUrl, setEditUrl] = useState('')
  const [editKey, setEditKey] = useState('')

  const loadPanels = useCallback(async () => {
    setError('')
    try {
      const res = await api<{ panels: RemnaPanel[] }>('/api/remna-panels')
      setPanels(Array.isArray(res.panels) ? res.panels : [])
      setLoading(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить панели')
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadPanels()
  }, [loadPanels])

  async function onAdd(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api<RemnaPanel>('/api/remna-panels', {
        method: 'POST',
        body: JSON.stringify({
          name: name.trim(),
          base_url: baseUrl.trim(),
          api_key: apiKey.trim(),
        }),
      })
      setName('')
      setBaseUrl('')
      setApiKey('')
      await loadPanels()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось добавить панель')
    } finally {
      setBusy(false)
    }
  }

  function startEdit(p: RemnaPanel) {
    setEditingId(p.id)
    setEditName(p.name)
    setEditUrl(p.base_url)
    setEditKey('')
  }

  async function onSaveEdit(e: FormEvent) {
    e.preventDefault()
    if (!editingId) return
    setBusy(true)
    setError('')
    try {
      const body: { name: string; base_url: string; api_key?: string } = {
        name: editName.trim(),
        base_url: editUrl.trim(),
      }
      if (editKey.trim()) body.api_key = editKey.trim()
      await api<{ ok: boolean; panel: RemnaPanel }>(`/api/remna-panels/${editingId}`, {
        method: 'PUT',
        body: JSON.stringify(body),
      })
      setEditingId(null)
      await loadPanels()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить')
    } finally {
      setBusy(false)
    }
  }

  async function onDelete(id: string, panelName: string) {
    if (!confirm(`Удалить панель «${panelName}»?`)) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/remna-panels/${id}`, { method: 'DELETE' })
      if (editingId === id) setEditingId(null)
      await loadPanels()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="panel">
      <div className="row" style={{ justifyContent: 'space-between', marginBottom: '0.5rem' }}>
        <h2 style={{ margin: 0 }}>Панели Remnawave</h2>
        <button className="btn btn-sm" type="button" onClick={() => void loadPanels()}>
          Обновить
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p className="muted">Загрузка…</p>
      ) : panels.length === 0 ? (
        <p className="muted">Панелей пока нет — добавьте ниже.</p>
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Имя</th>
                <th>URL</th>
                <th>Ключ</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {panels.map((p) =>
                editingId === p.id ? (
                  <tr key={p.id}>
                    <td colSpan={4}>
                      <form className="stack" onSubmit={onSaveEdit}>
                        <div className="row" style={{ alignItems: 'flex-end' }}>
                          <div className="field" style={{ minWidth: 120 }}>
                            <label htmlFor={`edit-name-${p.id}`}>Имя</label>
                            <input
                              id={`edit-name-${p.id}`}
                              value={editName}
                              onChange={(e) => setEditName(e.target.value)}
                              required
                            />
                          </div>
                          <div className="field" style={{ flex: '1 1 14rem', minWidth: 160 }}>
                            <label htmlFor={`edit-url-${p.id}`}>URL</label>
                            <input
                              id={`edit-url-${p.id}`}
                              className="mono"
                              value={editUrl}
                              onChange={(e) => setEditUrl(e.target.value)}
                              required
                              placeholder="https://panel.example.com"
                            />
                          </div>
                          <div className="field" style={{ minWidth: 140 }}>
                            <label htmlFor={`edit-key-${p.id}`}>API key</label>
                            <input
                              id={`edit-key-${p.id}`}
                              type="password"
                              value={editKey}
                              onChange={(e) => setEditKey(e.target.value)}
                              placeholder="оставить пустым"
                              autoComplete="off"
                            />
                          </div>
                          <button className="btn btn-sm btn-primary" type="submit" disabled={busy}>
                            Сохранить
                          </button>
                          <button
                            className="btn btn-sm"
                            type="button"
                            disabled={busy}
                            onClick={() => setEditingId(null)}
                          >
                            Отмена
                          </button>
                        </div>
                      </form>
                    </td>
                  </tr>
                ) : (
                  <tr key={p.id}>
                    <td>{p.name}</td>
                    <td className="mono muted">{p.base_url}</td>
                    <td className="muted">{p.has_api_key ? 'ключ задан' : 'нет ключа'}</td>
                    <td>
                      <div className="row" style={{ justifyContent: 'flex-end' }}>
                        <button
                          className="btn btn-sm"
                          type="button"
                          disabled={busy}
                          onClick={() => startEdit(p)}
                        >
                          Изменить
                        </button>
                        <button
                          className="btn btn-sm btn-danger"
                          type="button"
                          disabled={busy}
                          onClick={() => void onDelete(p.id, p.name)}
                        >
                          Удалить
                        </button>
                      </div>
                    </td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}

      <h3 style={{ marginTop: '1rem' }}>Добавить панель</h3>
      <form className="stack" onSubmit={onAdd}>
        <div className="row" style={{ alignItems: 'flex-end' }}>
          <div className="field" style={{ minWidth: 120 }}>
            <label htmlFor="remna-name">Имя</label>
            <input
              id="remna-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              placeholder="main"
            />
          </div>
          <div className="field" style={{ flex: '1 1 14rem', minWidth: 160 }}>
            <label htmlFor="remna-url">URL (https)</label>
            <input
              id="remna-url"
              className="mono"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              required
              placeholder="https://panel.example.com"
            />
          </div>
          <div className="field" style={{ minWidth: 140 }}>
            <label htmlFor="remna-key">API key</label>
            <input
              id="remna-key"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              required
              autoComplete="off"
            />
          </div>
          <button className="btn btn-primary" type="submit" disabled={busy}>
            {busy ? '…' : 'Добавить'}
          </button>
        </div>
      </form>
    </section>
  )
}

function ProvidersSection({
  providers,
  onChange,
}: {
  providers: Provider[]
  onChange: () => void
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState('')
  const [favicon, setFavicon] = useState('')
  const [loginUrl, setLoginUrl] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editName, setEditName] = useState('')
  const [editFavicon, setEditFavicon] = useState('')
  const [editLogin, setEditLogin] = useState('')
  const [accountDrafts, setAccountDrafts] = useState<Record<string, string>>({})

  async function onAdd(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api<Provider>('/api/providers', {
        method: 'POST',
        body: JSON.stringify({
          name: name.trim(),
          favicon_url: favicon.trim(),
          login_url: loginUrl.trim(),
        }),
      })
      setName('')
      setFavicon('')
      setLoginUrl('')
      onChange()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось добавить провайдера')
    } finally {
      setBusy(false)
    }
  }

  function startEdit(p: Provider) {
    setEditingId(p.id)
    setEditName(p.name)
    setEditFavicon(p.favicon_url)
    setEditLogin(p.login_url)
  }

  async function onSaveEdit(e: FormEvent) {
    e.preventDefault()
    if (!editingId) return
    setBusy(true)
    setError('')
    try {
      await api<{ ok: boolean; provider: Provider }>(`/api/providers/${editingId}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: editName.trim(),
          favicon_url: editFavicon.trim(),
          login_url: editLogin.trim(),
        }),
      })
      setEditingId(null)
      onChange()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить')
    } finally {
      setBusy(false)
    }
  }

  async function onDelete(id: string, providerName: string) {
    if (!confirm(`Удалить провайдера «${providerName}»?`)) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/providers/${id}`, { method: 'DELETE' })
      if (editingId === id) setEditingId(null)
      onChange()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить')
    } finally {
      setBusy(false)
    }
  }

  async function onAddAccount(providerId: string) {
    const login = (accountDrafts[providerId] || '').trim()
    if (!login) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/providers/${providerId}/accounts`, {
        method: 'POST',
        body: JSON.stringify({ login }),
      })
      setAccountDrafts((m) => ({ ...m, [providerId]: '' }))
      onChange()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось добавить аккаунт')
    } finally {
      setBusy(false)
    }
  }

  async function onDeleteAccount(accountId: string, login: string) {
    if (!confirm(`Удалить аккаунт «${login}»?`)) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/provider-accounts/${accountId}`, { method: 'DELETE' })
      onChange()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить аккаунт')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="panel remna-panels-top">
      <div className="row" style={{ justifyContent: 'space-between', marginBottom: '0.5rem' }}>
        <h2 style={{ margin: 0 }}>Провайдеры</h2>
      </div>
      <p className="muted" style={{ marginTop: 0 }}>
        Имя и иконка показываются рядом с IP ноды. У каждого провайдера можно добавить аккаунты —
        на ноде выбирается провайдер и аккаунт, логин виден по наведению на «!».
      </p>
      {error && <p className="error">{error}</p>}
      {providers.length === 0 ? (
        <p className="muted">Провайдеров пока нет — добавьте ниже.</p>
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Имя</th>
                <th>Favicon</th>
                <th>Вход</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {providers.map((p) =>
                editingId === p.id ? (
                  <tr key={p.id}>
                    <td colSpan={4}>
                      <form className="stack" onSubmit={onSaveEdit}>
                        <div className="row" style={{ alignItems: 'flex-end' }}>
                          <div className="field" style={{ minWidth: 120 }}>
                            <label htmlFor={`edit-prov-name-${p.id}`}>Имя</label>
                            <input
                              id={`edit-prov-name-${p.id}`}
                              value={editName}
                              onChange={(e) => setEditName(e.target.value)}
                              required
                            />
                          </div>
                          <div className="field" style={{ flex: '1 1 10rem', minWidth: 140 }}>
                            <label htmlFor={`edit-prov-fav-${p.id}`}>Favicon</label>
                            <input
                              id={`edit-prov-fav-${p.id}`}
                              className="mono"
                              value={editFavicon}
                              onChange={(e) => setEditFavicon(e.target.value)}
                              placeholder="https://hetzner.com"
                            />
                          </div>
                          <div className="field" style={{ flex: '1 1 10rem', minWidth: 140 }}>
                            <label htmlFor={`edit-prov-login-${p.id}`}>Ссылка для входа</label>
                            <input
                              id={`edit-prov-login-${p.id}`}
                              className="mono"
                              value={editLogin}
                              onChange={(e) => setEditLogin(e.target.value)}
                              placeholder="https://cloud.hetzner.com"
                            />
                          </div>
                          <button className="btn btn-sm btn-primary" type="submit" disabled={busy}>
                            Сохранить
                          </button>
                          <button
                            className="btn btn-sm"
                            type="button"
                            disabled={busy}
                            onClick={() => setEditingId(null)}
                          >
                            Отмена
                          </button>
                        </div>
                      </form>
                    </td>
                  </tr>
                ) : (
                  <Fragment key={p.id}>
                    <tr>
                      <td>
                        <ProviderBadge
                          name={p.name}
                          favicon={p.favicon_url}
                          loginUrl={p.login_url}
                        />
                      </td>
                      <td className="mono muted">{p.favicon_url || '—'}</td>
                      <td className="mono muted">{p.login_url || '—'}</td>
                      <td>
                        <div className="row" style={{ justifyContent: 'flex-end' }}>
                          <button
                            className="btn btn-sm"
                            type="button"
                            disabled={busy}
                            onClick={() => startEdit(p)}
                          >
                            Изменить
                          </button>
                          <button
                            className="btn btn-sm btn-danger"
                            type="button"
                            disabled={busy}
                            onClick={() => void onDelete(p.id, p.name)}
                          >
                            Удалить
                          </button>
                        </div>
                      </td>
                    </tr>
                    <tr>
                      <td colSpan={4}>
                        <div className="provider-accounts-block">
                          <div className="muted" style={{ fontSize: '0.78rem', marginBottom: '0.35rem' }}>
                            Аккаунты
                          </div>
                          {(p.accounts?.length ?? 0) > 0 ? (
                            <div className="provider-accounts-list">
                              {(p.accounts ?? []).map((a) => (
                                <div key={a.id} className="provider-account-row">
                                  <span className="mono">{a.login}</span>
                                  <button
                                    className="btn btn-sm btn-danger"
                                    type="button"
                                    disabled={busy}
                                    onClick={() => void onDeleteAccount(a.id, a.login)}
                                  >
                                    Удалить
                                  </button>
                                </div>
                              ))}
                            </div>
                          ) : (
                            <p className="muted" style={{ margin: '0 0 0.35rem', fontSize: '0.8rem' }}>
                              Пока нет аккаунтов
                            </p>
                          )}
                          <form
                            className="row"
                            style={{ alignItems: 'flex-end', gap: '0.45rem' }}
                            onSubmit={(e) => {
                              e.preventDefault()
                              void onAddAccount(p.id)
                            }}
                          >
                            <div className="field" style={{ flex: '1 1 12rem', minWidth: 140, margin: 0 }}>
                              <label htmlFor={`acc-${p.id}`}>Логин / email</label>
                              <input
                                id={`acc-${p.id}`}
                                className="mono"
                                value={accountDrafts[p.id] || ''}
                                onChange={(e) =>
                                  setAccountDrafts((m) => ({ ...m, [p.id]: e.target.value }))
                                }
                                placeholder="user@example.com"
                                required
                              />
                            </div>
                            <button className="btn btn-sm btn-primary" type="submit" disabled={busy}>
                              Добавить аккаунт
                            </button>
                          </form>
                        </div>
                      </td>
                    </tr>
                  </Fragment>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}

      <form className="stack remna-panels-add" onSubmit={onAdd}>
        <div className="row" style={{ alignItems: 'flex-end' }}>
          <div className="field" style={{ minWidth: 120 }}>
            <label htmlFor="prov-name">Имя</label>
            <input
              id="prov-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              placeholder="Введите имя провайдера"
            />
          </div>
          <div className="field" style={{ flex: '1 1 10rem', minWidth: 140 }}>
            <label htmlFor="prov-favicon">Ссылка на Favicon</label>
            <input
              id="prov-favicon"
              className="mono"
              value={favicon}
              onChange={(e) => setFavicon(e.target.value)}
              placeholder="https://hetzner.com"
            />
          </div>
          <div className="field" style={{ flex: '1 1 10rem', minWidth: 140 }}>
            <label htmlFor="prov-login">Ссылка для входа</label>
            <input
              id="prov-login"
              className="mono"
              value={loginUrl}
              onChange={(e) => setLoginUrl(e.target.value)}
              placeholder="https://cloud.hetzner.com"
            />
          </div>
          <button className="btn btn-primary" type="submit" disabled={busy}>
            {busy ? '…' : 'Создать'}
          </button>
        </div>
      </form>
    </section>
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

function AddNodeWizard({
  onClose,
  onDone,
  resume,
}: {
  onClose: () => void
  onDone: () => void
  resume?: { node: Node; bundle: Bundle }
}) {
  const [step, setStep] = useState(resume ? 2 : 1)
  const [name, setName] = useState(resume?.node.name ?? '')
  const [host, setHost] = useState(resume?.bundle.host ?? '')
  const [port, setPort] = useState(String(resume?.bundle.port ?? 47893))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [node, setNode] = useState<Node | null>(resume?.node ?? null)
  const [bundle, setBundle] = useState<Bundle | null>(resume?.bundle ?? null)
  const [fileKey, setFileKey] = useState('docker-compose.yml')
  const [copied, setCopied] = useState('')
  const [connectMsg, setConnectMsg] = useState('')
  const [connectFailed, setConnectFailed] = useState(false)

  async function onNext(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api<{ node: Node; bundle: Bundle }>('/api/nodes/provision', {
        method: 'POST',
        body: JSON.stringify({
          name,
          host,
          port: Number(port) || 47893,
        }),
      })
      setNode(res.node)
      setBundle(res.bundle)
      setFileKey('docker-compose.yml')
      setStep(2)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось создать ноду')
    } finally {
      setBusy(false)
    }
  }

  async function copyText(label: string, text: string) {
    setError('')
    const ok = await copyToClipboard(text)
    if (ok) {
      setCopied(label)
      setTimeout(() => setCopied(''), 1500)
    } else {
      setError('Не удалось скопировать')
    }
  }

  function downloadFile(filename: string, text: string) {
    downloadTextFile(filename, text)
  }

  async function onConnect() {
    if (!node) return
    setBusy(true)
    setError('')
    setConnectMsg('')
    setConnectFailed(false)
    try {
      const res = await api<{
        ok: boolean
        online: boolean
        error?: string
        node: Node
      }>(`/api/nodes/${node.id}/connect`, { method: 'POST' })
      setNode(res.node)
      if (res.ok && res.online) {
        setConnectMsg('Связь есть — нода онлайн')
        setConnectFailed(false)
        setStep(3)
      } else {
        setConnectFailed(true)
        setError(
          translateError(res.error || '') ||
            'Нода недоступна — запущен ли docker compose?',
        )
      }
    } catch (err) {
      setConnectFailed(true)
      setError(err instanceof Error ? err.message : 'Ошибка подключения')
    } finally {
      setBusy(false)
    }
  }

  const fileContent = bundle?.files?.[fileKey] ?? ''

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal modal-wide stack"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <h3 style={{ margin: 0 }}>Добавить ноду</h3>
          <span className="muted">Шаг {step} / 3</span>
        </div>

        {step === 1 && (
          <form className="stack" onSubmit={onNext}>
            <p className="muted" style={{ margin: 0 }}>
              Укажите, как панель будет достучаться до этой ноды HAProxy. Далее
              получите готовый docker compose с токеном.
            </p>
            <div className="field">
              <label htmlFor="name">Имя</label>
              <input
                id="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                placeholder="edge-1"
                autoFocus
              />
            </div>
            <div className="field">
              <label htmlFor="host">IP или домен</label>
              <input
                id="host"
                value={host}
                onChange={(e) => setHost(e.target.value)}
                required
                placeholder="203.0.113.10 или node.example.com"
              />
            </div>
            <div className="field">
              <label htmlFor="port">Порт управления (панель)</label>
              <input
                id="port"
                type="number"
                min={1}
                max={65535}
                value={port}
                onChange={(e) => setPort(e.target.value)}
                required
              />
              <p className="muted" style={{ margin: '0.35rem 0 0', fontSize: '0.85rem' }}>
                Клиенты — 8443 (HAProxy). Панель ходит к агенту напрямую по HTTP
                на этом порту (по умолчанию 47893), не через HAProxy.
              </p>
            </div>
            {error && <p className="error">{error}</p>}
            <div className="row">
              <button className="btn btn-primary" type="submit" disabled={busy}>
                {busy ? 'Создание…' : 'Далее'}
              </button>
              <button className="btn" type="button" onClick={onClose}>
                Отмена
              </button>
            </div>
          </form>
        )}

        {step === 2 && bundle && node && (
          <div className="stack">
            <div className="status-banner unknown">
              <StatusBadge status={node.status || 'unknown'} large />
              <span>
                Статус: {statusLabel(node.status || 'unknown')}. Скопируйте файлы на
                VPS, запустите compose, затем нажмите «Проверить связь».
              </span>
            </div>
            <p className="muted" style={{ margin: 0 }}>
              Панель будет обращаться к{' '}
              <span className="mono">{bundle.url}</span>
            </p>
            <div className="field">
              <label>Токен (уже вписан в compose)</label>
              <div className="row">
                <input className="mono" readOnly value={bundle.token} />
                <button
                  className="btn btn-sm"
                  type="button"
                  onClick={() => void copyText('token', bundle.token)}
                >
                  {copied === 'token' ? 'Скопировано' : 'Копировать'}
                </button>
              </div>
            </div>
            <div className="field">
              <label>Файл</label>
              <select
                value={fileKey}
                onChange={(e) => setFileKey(e.target.value)}
              >
                {Object.keys(bundle.files).map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
              </select>
            </div>
            <pre className="pre pre-tall">{fileContent}</pre>
            <div className="row">
              <button
                className="btn btn-sm"
                type="button"
                onClick={() => void copyText(fileKey, fileContent)}
              >
                {copied === fileKey ? 'Скопировано' : `Копировать ${fileKey}`}
              </button>
              <button
                className="btn btn-sm"
                type="button"
                onClick={() => downloadFile(fileKey, fileContent)}
              >
                Скачать файл
              </button>
              <button
                className="btn btn-sm"
                type="button"
                onClick={() => void copyText('commands', bundle.commands)}
              >
                {copied === 'commands' ? 'Скопировано' : 'Копировать команды'}
              </button>
            </div>
            <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
              На сервере: создайте <span className="mono">/opt/hapanel-node</span>,
              вставьте <span className="mono">docker-compose.yml</span> и конфиги
              haproxy, сделайте{' '}
              <span className="mono">docker pull {bundle.agent_image}</span>, затем{' '}
              <span className="mono">docker compose up -d</span>.
            </p>
            {error && (
              <div className="connect-result fail">
                <strong>Нет связи</strong>
                <p className="error" style={{ margin: '0.35rem 0 0' }}>
                  {error}
                </p>
              </div>
            )}
            {connectMsg && !connectFailed && (
              <div className="connect-result ok">
                <strong>{connectMsg}</strong>
              </div>
            )}
            <div className="row">
              <button
                className="btn btn-primary"
                type="button"
                disabled={busy}
                onClick={() => void onConnect()}
              >
                {busy ? (
                  <span className="btn-spinner-label">
                    <span className="spinner" aria-hidden />
                    Проверка…
                  </span>
                ) : (
                  'Проверить связь'
                )}
              </button>
              <button className="btn" type="button" onClick={onDone}>
                Закрыть
              </button>
            </div>
          </div>
        )}

        {step === 3 && node && (
          <div className="stack">
            <div className="connect-result ok">
              <StatusBadge status="online" large />
              <p className="ok-msg" style={{ margin: 0 }}>
                Нода <strong>{node.name}</strong> онлайн — связь есть.
              </p>
            </div>
            <p className="muted" style={{ margin: 0 }}>
              URL: <span className="mono">{node.url}</span>
            </p>
            <div className="row">
              <Link className="btn btn-primary" to={`/nodes/${node.id}`} onClick={onDone}>
                Открыть ноду
              </Link>
              <button className="btn" type="button" onClick={onDone}>
                Готово
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
