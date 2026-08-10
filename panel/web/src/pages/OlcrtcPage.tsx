import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  api,
  formatLastSeen,
  statusLabel,
  translateError,
  type OlcrtcInstance,
  type OlcrtcNode,
  type Provider,
} from '../api'
import { copyToClipboard } from '../clipboard'
import BrandNav from '../components/BrandNav'
import CountryPicker from '../components/CountryPicker'
import HelpTip from '../components/HelpTip'
import { ProviderBadge } from './NodesPage'

const PROVIDERS = ['jitsi', 'telemost', 'wbstream'] as const
const TRANSPORTS = ['datachannel', 'vp8channel', 'seichannel', 'videochannel'] as const

/** Compatible transports per olcRTC auth.provider (OpenLibreCommunity matrix). */
const TRANSPORTS_BY_PROVIDER: Record<(typeof PROVIDERS)[number], readonly string[]> = {
  jitsi: ['datachannel', 'vp8channel', 'seichannel', 'videochannel'],
  telemost: ['vp8channel', 'videochannel'], // no datachannel / seichannel
  wbstream: ['vp8channel', 'seichannel', 'videochannel', 'datachannel'], // DC needs moderator token
}

const DEFAULT_TRANSPORT: Record<(typeof PROVIDERS)[number], string> = {
  jitsi: 'datachannel',
  telemost: 'vp8channel',
  wbstream: 'vp8channel',
}

function transportsForProvider(provider: string): readonly string[] {
  if (provider in TRANSPORTS_BY_PROVIDER) {
    return TRANSPORTS_BY_PROVIDER[provider as (typeof PROVIDERS)[number]]
  }
  return TRANSPORTS
}

type Tab = 'nodes' | 'agent'

type AgentMsg = {
  at: string
  role: string
  text: string
  step?: string
}

function errMsg(err: unknown): string {
  if (err instanceof Error) return err.message
  return translateError(String(err))
}

function formatTs(v: string | number | null | undefined): string {
  if (v == null || v === '' || v === 0) return '—'
  if (typeof v === 'number') {
    const ms = v < 1e12 ? v * 1000 : v
    return formatLastSeen(new Date(ms).toISOString())
  }
  return formatLastSeen(v)
}

function truncate(s: string, n = 36): string {
  const t = (s || '').trim()
  if (t.length <= n) return t || '—'
  return t.slice(0, n - 1) + '…'
}

function StatusBadge({ status }: { status: string }) {
  const s = status || 'unknown'
  return <span className={`badge badge-status ${s}`}>{statusLabel(s)}</span>
}

export default function OlcrtcPage() {
  const [tab, setTab] = useState<Tab>('nodes')
  const [nodes, setNodes] = useState<OlcrtcNode[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [instances, setInstances] = useState<OlcrtcInstance[]>([])
  const [openNodeId, setOpenNodeId] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [okMsg, setOkMsg] = useState('')

  const [instName, setInstName] = useState('')
  const [instProvider, setInstProvider] = useState<string>('jitsi')
  const [instTransport, setInstTransport] = useState<string>('datachannel')
  const [instRoom, setInstRoom] = useState('')
  const [instComment, setInstComment] = useState('')

  const openNode = useMemo(
    () => nodes.find((n) => n.id === openNodeId) ?? null,
    [nodes, openNodeId],
  )

  const loadNodes = useCallback(async () => {
    const res = await api<{ nodes?: OlcrtcNode[] } | OlcrtcNode[]>('/api/olcrtc/nodes')
    const list = Array.isArray(res) ? res : Array.isArray(res.nodes) ? res.nodes : []
    list.sort((a, b) => Number(b.updated_at || 0) - Number(a.updated_at || 0))
    setNodes(list)
    return list
  }, [])

  const loadProviders = useCallback(async () => {
    const res = await api<{ providers?: Provider[] }>('/api/providers')
    setProviders(Array.isArray(res.providers) ? res.providers : [])
  }, [])

  const loadInstances = useCallback(async (nodeId: string) => {
    if (!nodeId) {
      setInstances([])
      return []
    }
    const res = await api<{ instances?: OlcrtcInstance[] } | OlcrtcInstance[]>(
      `/api/olcrtc/nodes/${encodeURIComponent(nodeId)}/instances`,
    )
    const list = Array.isArray(res) ? res : Array.isArray(res.instances) ? res.instances : []
    setInstances(list)
    return list
  }, [])

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError('')
      try {
        await Promise.all([loadNodes(), loadProviders()])
      } catch (err) {
        if (!cancelled) setError(errMsg(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [loadNodes, loadProviders])

  async function openNodeView(id: string) {
    setOpenNodeId(id)
    setError('')
    setOkMsg('')
    setTab('nodes')
    try {
      await loadInstances(id)
    } catch (err) {
      setError(errMsg(err))
    }
  }

  async function onRefreshStatus(id: string) {
    setBusy(true)
    setError('')
    setOkMsg('')
    try {
      await api(`/api/olcrtc/nodes/${encodeURIComponent(id)}/refresh-status`, { method: 'POST' })
      await loadNodes()
      setOkMsg('Статус обновлён')
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onRestart(id: string) {
    setBusy(true)
    setError('')
    setOkMsg('')
    try {
      await api(`/api/olcrtc/nodes/${encodeURIComponent(id)}/restart`, { method: 'POST' })
      await loadNodes()
      if (openNodeId === id) await loadInstances(id)
      setOkMsg('Restart отправлен olcnode')
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onSetCountry(id: string, country: string) {
    setBusy(true)
    setError('')
    try {
      await api(`/api/olcrtc/nodes/${encodeURIComponent(id)}`, {
        method: 'PUT',
        body: JSON.stringify({ country }),
      })
      await loadNodes()
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onSetProvider(id: string, providerId: string) {
    setBusy(true)
    setError('')
    try {
      await api(`/api/olcrtc/nodes/${encodeURIComponent(id)}`, {
        method: 'PUT',
        body: JSON.stringify({ provider_id: providerId, provider_account_id: '' }),
      })
      await loadNodes()
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onSetProviderAccount(id: string, accountId: string) {
    setBusy(true)
    setError('')
    try {
      await api(`/api/olcrtc/nodes/${encodeURIComponent(id)}`, {
        method: 'PUT',
        body: JSON.stringify({ provider_account_id: accountId }),
      })
      await loadNodes()
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onAddInstance(e: FormEvent) {
    e.preventDefault()
    if (!openNodeId) return
    setBusy(true)
    setError('')
    setOkMsg('')
    try {
      await api(`/api/olcrtc/nodes/${encodeURIComponent(openNodeId)}/instances`, {
        method: 'POST',
        body: JSON.stringify({
          name: instName.trim(),
          provider: instProvider,
          transport: instTransport,
          room_id: instRoom.trim(),
          comment: instComment.trim(),
        }),
      })
      setInstName('')
      setInstRoom('')
      setInstComment('')
      await loadInstances(openNodeId)
      setOkMsg('Инстанс создан на ноде (key сгенерирован)')
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onRestartInstance(id: string, name: string) {
    setBusy(true)
    setError('')
    setOkMsg('')
    try {
      const q = openNodeId ? `?node_id=${encodeURIComponent(openNodeId)}` : ''
      await api(`/api/olcrtc/instances/${encodeURIComponent(id)}/restart${q}`, { method: 'POST' })
      if (openNodeId) await loadInstances(openNodeId)
      setOkMsg(`Комната «${name}» перезапущена`)
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onDeleteInstance(id: string, name: string) {
    if (!window.confirm(`Удалить инстанс «${name}»?`)) return
    setBusy(true)
    setError('')
    setOkMsg('')
    try {
      const q = openNodeId ? `?node_id=${encodeURIComponent(openNodeId)}` : ''
      await api(`/api/olcrtc/instances/${encodeURIComponent(id)}${q}`, { method: 'DELETE' })
      if (openNodeId) await loadInstances(openNodeId)
      setOkMsg('Инстанс удалён')
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  async function onCopyUri(id: string) {
    setBusy(true)
    setError('')
    setOkMsg('')
    try {
      const q = openNodeId ? `?node_id=${encodeURIComponent(openNodeId)}` : ''
      const res = await api<{ uri?: string }>(`/api/olcrtc/instances/${encodeURIComponent(id)}/uri${q}`)
      const uri = (res.uri || '').trim()
      if (!uri) throw new Error('Пустой URI')
      const ok = await copyToClipboard(uri)
      setOkMsg(ok ? 'URI скопирован' : uri)
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="shell">
      <header className="topbar">
        <BrandNav active="olcnode" />
        <div className="row">
          <button
            className="btn btn-sm"
            type="button"
            disabled={busy || loading}
            onClick={() => {
              setLoading(true)
              void (async () => {
                setError('')
                setOkMsg('')
                try {
                  await loadNodes()
                  if (openNodeId) await loadInstances(openNodeId)
                } catch (err) {
                  setError(errMsg(err))
                } finally {
                  setLoading(false)
                }
              })()
            }}
          >
            Обновить
          </button>
        </div>
      </header>

      <nav className="page-tabs" aria-label="Разделы olcnode">
        <button
          type="button"
          className={`page-tab${tab === 'nodes' ? ' active' : ''}`}
          onClick={() => {
            setTab('nodes')
            setOpenNodeId('')
            setInstances([])
            setError('')
            setOkMsg('')
          }}
        >
          Ноды
        </button>
        <button
          type="button"
          className={`page-tab${tab === 'agent' ? ' active' : ''}`}
          onClick={() => {
            setTab('agent')
            setError('')
            setOkMsg('')
          }}
        >
          Агент
        </button>
      </nav>

      {error ? <p className="error">{error}</p> : null}
      {okMsg ? <p className="ok-msg">{okMsg}</p> : null}

      {tab === 'agent' ? (
        <OlcnodeDeployAgent
          providers={providers}
          onDone={() => {
            void loadNodes()
          }}
          onOpenNode={(id) => {
            void openNodeView(id)
          }}
        />
      ) : openNode ? (
        <section className="panel">
          <div className="row" style={{ justifyContent: 'space-between', flexWrap: 'wrap', gap: '0.75rem' }}>
            <div className="olc-node-header">
              <button
                className="btn btn-sm btn-ghost"
                type="button"
                onClick={() => {
                  setOpenNodeId('')
                  setInstances([])
                  setOkMsg('')
                  setError('')
                }}
              >
                ← К нодам
              </button>
              <h2 className="heading-with-tip olc-node-title">
                {openNode.name}
                <HelpTip text="Инстансы живут на ноде: она подключается к комнате. Copy URI — ссылка для клиентов." />
              </h2>
            </div>
            <div className="row" style={{ alignItems: 'center', flexWrap: 'wrap' }}>
              {openNode.provider_name ? (
                <ProviderBadge
                  name={openNode.provider_name}
                  favicon={openNode.provider_favicon}
                  loginUrl={openNode.provider_login_url}
                  accountLogin={openNode.provider_account_login}
                />
              ) : null}
              <StatusBadge status={openNode.status} />
              <button
                className="btn btn-sm"
                type="button"
                disabled={busy}
                onClick={() => void onRefreshStatus(openNode.id)}
              >
                Refresh
              </button>
              <button
                className="btn btn-sm"
                type="button"
                disabled={busy}
                onClick={() => void onRestart(openNode.id)}
              >
                Restart
              </button>
            </div>
          </div>

          <div className="node-detail-selects" style={{ marginBottom: '1rem' }}>
            <div className="field node-remna-panel-field">
              <label htmlFor="olc-node-provider">Провайдер</label>
              <select
                id="olc-node-provider"
                value={openNode.provider_id || ''}
                disabled={busy}
                onChange={(e) => void onSetProvider(openNode.id, e.target.value)}
              >
                <option value="">— не выбран —</option>
                {providers.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
            {openNode.provider_id ? (
              <div className="field node-remna-panel-field">
                <label htmlFor="olc-node-account">Аккаунт</label>
                <select
                  id="olc-node-account"
                  value={openNode.provider_account_id || ''}
                  disabled={busy}
                  onChange={(e) => void onSetProviderAccount(openNode.id, e.target.value)}
                >
                  <option value="">— не выбран —</option>
                  {(providers.find((p) => p.id === openNode.provider_id)?.accounts ?? []).map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.login}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}
          </div>

          {instances.length === 0 ? (
            <p className="muted">Инстансов нет — создайте ниже.</p>
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Имя</th>
                    <th>Provider</th>
                    <th>Transport</th>
                    <th>Room</th>
                    <th>Comment</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {instances.map((inst) => (
                    <tr key={inst.id}>
                      <td>{inst.name}</td>
                      <td className="mono">{inst.provider}</td>
                      <td className="mono">{inst.transport}</td>
                      <td className="mono muted" title={inst.room_id}>
                        {truncate(inst.room_id, 28)}
                      </td>
                      <td className="muted" title={inst.comment || ''}>
                        {truncate(inst.comment || '', 24)}
                      </td>
                      <td>
                        <div className="row" style={{ justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                          <button
                            className="btn btn-sm"
                            type="button"
                            disabled={busy}
                            onClick={() => void onRestartInstance(inst.id, inst.name)}
                          >
                            Restart
                          </button>
                          <button
                            className="btn btn-sm"
                            type="button"
                            disabled={busy}
                            onClick={() => void onCopyUri(inst.id)}
                          >
                            Copy URI
                          </button>
                          <button
                            className="btn btn-sm btn-danger"
                            type="button"
                            disabled={busy}
                            onClick={() => void onDeleteInstance(inst.id, inst.name)}
                          >
                            Delete
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <form className="stack remna-panels-add" onSubmit={onAddInstance}>
            <h3 className="heading-with-tip">
              Создать инстанс на ноде
              <HelpTip text="Key hex генерируется автоматически. Transport зависит от выбранного provider." />
            </h3>
            <div className="row" style={{ alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div className="field" style={{ minWidth: 120 }}>
                <label htmlFor="olc-inst-name">Имя</label>
                <input
                  id="olc-inst-name"
                  value={instName}
                  onChange={(e) => setInstName(e.target.value)}
                  required
                />
              </div>
              <div className="field" style={{ minWidth: 120 }}>
                <label htmlFor="olc-inst-provider">Provider</label>
                <select
                  id="olc-inst-provider"
                  value={instProvider}
                  onChange={(e) => {
                    const p = e.target.value
                    setInstProvider(p)
                    const allowed = transportsForProvider(p)
                    if (!allowed.includes(instTransport)) {
                      setInstTransport(
                        DEFAULT_TRANSPORT[p as (typeof PROVIDERS)[number]] || allowed[0] || '',
                      )
                    }
                  }}
                >
                  {PROVIDERS.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field" style={{ minWidth: 140 }}>
                <label htmlFor="olc-inst-transport">Transport</label>
                <select
                  id="olc-inst-transport"
                  value={instTransport}
                  onChange={(e) => setInstTransport(e.target.value)}
                >
                  {transportsForProvider(instProvider).map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field" style={{ flex: '1 1 10rem', minWidth: 160 }}>
                <label htmlFor="olc-inst-room">Room</label>
                <input
                  id="olc-inst-room"
                  className="mono"
                  value={instRoom}
                  onChange={(e) => setInstRoom(e.target.value)}
                  required
                  placeholder="https://meet.jit.si/room"
                />
              </div>
              <div className="field" style={{ minWidth: 120 }}>
                <label htmlFor="olc-inst-comment">Comment</label>
                <input
                  id="olc-inst-comment"
                  value={instComment}
                  onChange={(e) => setInstComment(e.target.value)}
                />
              </div>
              <button className="btn btn-sm btn-primary" type="submit" disabled={busy}>
                Создать
              </button>
            </div>
          </form>
        </section>
      ) : (
        <section className="panel">
          <h2 className="heading-with-tip">
            Ноды
            <HelpTip text="Список olcnode. Провайдера и аккаунт можно задать при открытии ноды или при деплое." />
          </h2>
          {loading ? (
            <p className="muted">Загрузка…</p>
          ) : nodes.length === 0 ? (
            <p className="muted">Нод нет — откройте «Агент» и поставьте olcnode на VPS.</p>
          ) : (
            <div className="table-wrap">
              <table className="table table-clickable">
                <thead>
                  <tr>
                    <th>Имя</th>
                    <th>Host</th>
                    <th>olcnode</th>
                    <th>Статус</th>
                    <th>Last seen</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {nodes.map((n) => (
                    <tr key={n.id} className="row-click" onClick={() => void openNodeView(n.id)}>
                      <td>
                        <div className="name-cell-main" style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', flexWrap: 'wrap' }}>
                          <span onClick={(e) => e.stopPropagation()}>
                            <CountryPicker
                              value={n.country || ''}
                              onChange={(code) => void onSetCountry(n.id, code)}
                              disabled={busy}
                            />
                          </span>
                          {n.name}
                          {n.provider_name ? (
                            <span onClick={(e) => e.stopPropagation()}>
                              <ProviderBadge
                                name={n.provider_name}
                                favicon={n.provider_favicon}
                                loginUrl={n.provider_login_url}
                                accountLogin={n.provider_account_login}
                              />
                            </span>
                          ) : null}
                        </div>
                      </td>
                      <td className="mono muted">{n.host || '—'}</td>
                      <td className="mono muted" title={n.agent_url || ''}>
                        {truncate(n.agent_url || '', 28)}
                      </td>
                      <td>
                        <StatusBadge status={n.status} />
                        {n.last_error ? (
                          <div className="status-hint" title={n.last_error}>
                            {truncate(n.last_error, 48)}
                          </div>
                        ) : null}
                      </td>
                      <td className="muted">{formatTs(n.last_seen_at)}</td>
                      <td onClick={(e) => e.stopPropagation()}>
                        <div className="row" style={{ justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                          <button
                            className="btn btn-sm"
                            type="button"
                            disabled={busy}
                            onClick={() => void onRefreshStatus(n.id)}
                          >
                            Refresh
                          </button>
                          <button
                            className="btn btn-sm btn-primary"
                            type="button"
                            disabled={busy}
                            onClick={() => void openNodeView(n.id)}
                          >
                            Open
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
    </div>
  )
}

function OlcnodeDeployAgent({
  providers,
  onDone,
  onOpenNode,
}: {
  providers: Provider[]
  onDone: () => void
  onOpenNode: (id: string) => void
}) {
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [country, setCountry] = useState('')
  const [providerId, setProviderId] = useState('')
  const [accountId, setAccountId] = useState('')
  const [sshUser, setSshUser] = useState('root')
  const [sshPassword, setSshPassword] = useState('')
  const [sshPort, setSshPort] = useState('22')
  const [agentPort, setAgentPort] = useState('9201')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [jobId, setJobId] = useState<string | null>(null)
  const [status, setStatus] = useState('')
  const [nodeId, setNodeId] = useState('')
  const [messages, setMessages] = useState<AgentMsg[]>([])
  const logRef = useRef<HTMLDivElement | null>(null)
  const stickToBottomRef = useRef(true)

  useEffect(() => {
    if (!jobId) return
    let cancelled = false
    const tick = async () => {
      try {
        const res = await api<{
          job: { id: string; status: string; node_id?: string }
          messages: AgentMsg[]
        }>(`/api/olcrtc/deploy-jobs/${jobId}`)
        if (cancelled) return
        setStatus(res.job.status)
        setNodeId(res.job.node_id || '')
        setMessages(Array.isArray(res.messages) ? res.messages : [])
        if (res.job.status === 'succeeded' || res.job.status === 'failed') {
          setBusy(false)
          if (res.job.status === 'succeeded') {
            onDone()
            if (res.job.node_id) onOpenNode(res.job.node_id)
          }
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
  }, [jobId, onDone, onOpenNode])

  useEffect(() => {
    const el = logRef.current
    if (el && stickToBottomRef.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [messages])

  function onLogScroll() {
    const el = logRef.current
    if (!el) return
    const dist = el.scrollHeight - el.scrollTop - el.clientHeight
    stickToBottomRef.current = dist < 64
  }

  async function onStart(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    setMessages([])
    setStatus('queued')
    setNodeId('')
    setJobId(null)
    stickToBottomRef.current = true
    try {
      const res = await api<{ job: { id: string; status: string } }>('/api/olcrtc/nodes/deploy', {
        method: 'POST',
        body: JSON.stringify({
          name: name.trim(),
          host: host.trim(),
          country: country.trim() || undefined,
          provider_id: providerId || undefined,
          provider_account_id: accountId || undefined,
          ssh_user: sshUser.trim() || 'root',
          ssh_password: sshPassword,
          ssh_port: Number(sshPort) || 22,
          agent_port: Number(agentPort) || 9201,
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
      <h2 className="heading-with-tip" style={{ marginTop: 0 }}>
        Агент
        <HelpTip text="Панель по SSH ставит olcnode на VPS, сама генерирует token и подключает ноду. Это не hanode." />
      </h2>
      {error ? <p className="error">{error}</p> : null}

      <form className="stack agent-form" onSubmit={onStart}>
        <div className="row" style={{ alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <div className="field" style={{ minWidth: 120 }}>
            <label htmlFor="olc-ag-name">Имя ноды</label>
            <input
              id="olc-ag-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              disabled={busy}
              placeholder="olc1"
            />
          </div>
          <div className="field" style={{ minWidth: 140 }}>
            <label htmlFor="olc-ag-host">IP / хост VPS</label>
            <input
              id="olc-ag-host"
              className="mono"
              value={host}
              onChange={(e) => setHost(e.target.value)}
              required
              disabled={busy}
              placeholder="1.2.3.4"
            />
          </div>
          <div className="field" style={{ minWidth: 72 }}>
            <label>Страна</label>
            <div style={{ paddingTop: 2 }}>
              <CountryPicker value={country} onChange={setCountry} disabled={busy} />
            </div>
          </div>
          <div className="field" style={{ minWidth: 140 }}>
            <label htmlFor="olc-ag-provider">Провайдер</label>
            <select
              id="olc-ag-provider"
              value={providerId}
              disabled={busy}
              onChange={(e) => {
                setProviderId(e.target.value)
                setAccountId('')
              }}
            >
              <option value="">—</option>
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>
          {providerId ? (
            <div className="field" style={{ minWidth: 140 }}>
              <label htmlFor="olc-ag-account">Аккаунт</label>
              <select
                id="olc-ag-account"
                value={accountId}
                disabled={busy}
                onChange={(e) => setAccountId(e.target.value)}
              >
                <option value="">—</option>
                {(providers.find((p) => p.id === providerId)?.accounts ?? []).map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.login}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          <div className="field" style={{ minWidth: 90 }}>
            <label htmlFor="olc-ag-user">SSH user</label>
            <input
              id="olc-ag-user"
              value={sshUser}
              onChange={(e) => setSshUser(e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="field" style={{ minWidth: 140 }}>
            <label htmlFor="olc-ag-pass">SSH пароль</label>
            <input
              id="olc-ag-pass"
              type="password"
              value={sshPassword}
              onChange={(e) => setSshPassword(e.target.value)}
              required
              disabled={busy}
              autoComplete="off"
            />
          </div>
          <div className="field" style={{ width: 72 }}>
            <label htmlFor="olc-ag-ssh-port">SSH</label>
            <input
              id="olc-ag-ssh-port"
              className="mono"
              value={sshPort}
              onChange={(e) => setSshPort(e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="field" style={{ width: 88 }}>
            <label htmlFor="olc-ag-port">olcnode</label>
            <input
              id="olc-ag-port"
              className="mono"
              value={agentPort}
              onChange={(e) => setAgentPort(e.target.value)}
              disabled={busy}
              title="Порт API olcnode на VPS"
            />
          </div>
          <button className="btn btn-primary" type="submit" disabled={busy}>
            {busy ? 'Ставлю…' : 'Развернуть'}
          </button>
        </div>
      </form>

      {(status || messages.length > 0) && (
        <div className="agent-status muted">
          Статус: <span className="mono">{status || '—'}</span>
          {nodeId ? (
            <>
              {' '}
              · нода <span className="mono">{nodeId.slice(0, 8)}…</span>
            </>
          ) : null}
        </div>
      )}

      <div className="agent-chat" ref={logRef} aria-live="polite" onScroll={onLogScroll}>
        {messages.length === 0 ? (
          <p className="muted">Лог появится после запуска.</p>
        ) : (
          messages.map((m, i) => (
            <div key={`${m.at}-${i}`} className={`agent-msg role-${m.role}`}>
              <div className="agent-msg-meta">
                <span className="agent-msg-role">{m.role}</span>
                {m.step ? <span className="mono"> · {m.step}</span> : null}
              </div>
              <pre className="agent-msg-text">{m.text}</pre>
            </div>
          ))
        )}
      </div>
    </section>
  )
}
