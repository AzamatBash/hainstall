import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  api,
  formatLastSeen,
  Node,
  setToken,
  StatsSummary,
  statusLabel,
  translateError,
  userFacingStats,
} from '../api'

function StatusBadge({ status, large }: { status: string; large?: boolean }) {
  const s = status || 'unknown'
  return (
    <span className={`badge badge-status ${s}${large ? ' badge-lg' : ''}`}>
      {statusLabel(s)}
    </span>
  )
}

export default function NodesPage() {
  const navigate = useNavigate()
  const [nodes, setNodes] = useState<Node[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [showAdd, setShowAdd] = useState(false)
  const [statsMap, setStatsMap] = useState<Record<string, string>>({})
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)

  const loadStats = useCallback(async (list: Node[]) => {
    const next: Record<string, string> = {}
    await Promise.all(
      list.map(async (n) => {
        if (n.status !== 'online') {
          next[n.id] = '—'
          return
        }
        try {
          const stats = await api<StatsSummary>(`/api/nodes/${n.id}/stats`)
          const facing = userFacingStats(stats)
          next[n.id] =
            facing.active_sessions !== null ? String(facing.active_sessions) : '—'
        } catch {
          next[n.id] = '—'
        }
      }),
    )
    setStatsMap(next)
  }, [])

  const load = useCallback(async () => {
    setError('')
    try {
      const res = await api<{ nodes: Node[] }>('/api/nodes')
      const list = Array.isArray(res.nodes) ? res.nodes : []
      // Show the table immediately — do not block UI on slow connect probes.
      setNodes(list)
      setLoading(false)

      // Silent health check for each node so status badges update without clicking.
      const probed = await Promise.all(
        list.map(async (n) => {
          try {
            const conn = await api<{
              ok: boolean
              online: boolean
              node: Node
            }>(`/api/nodes/${n.id}/connect`, { method: 'POST' })
            return conn.node ?? { ...n, status: 'offline' as const }
          } catch {
            return { ...n, status: 'offline' as const }
          }
        }),
      )
      setNodes(probed.filter(Boolean))
      await loadStats(probed.filter(Boolean))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить ноды')
      setLoading(false)
    }
  }, [loadStats])

  useEffect(() => {
    void load()
  }, [load])

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

  async function onDelete(id: string, name: string) {
    setMenuOpenId(null)
    if (!confirm(`Удалить ноду «${name}»?`)) return
    try {
      await api(`/api/nodes/${id}`, { method: 'DELETE' })
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
    window.location.href = '/login'
  }

  return (
    <div className="shell">
      <header className="topbar">
        <div className="brand">
          ha<span>panel</span>
        </div>
        <div className="row">
          <button className="btn btn-sm" type="button" onClick={() => void load()}>
            Обновить
          </button>
          <button className="btn btn-sm btn-primary" type="button" onClick={() => setShowAdd(true)}>
            Добавить ноду
          </button>
          <button className="btn btn-sm btn-ghost" type="button" onClick={() => void logout()}>
            Выйти
          </button>
        </div>
      </header>

      <section className="panel">
        <h2>Ноды</h2>
        {error && <p className="error">{error}</p>}
        {loading ? (
          <p className="muted">Загрузка…</p>
        ) : (nodes?.length ?? 0) === 0 ? (
          <p className="muted">
            Нод пока нет. Добавьте первую — получите готовый docker compose.
          </p>
        ) : (
          <div className="table-wrap">
            <table className="table table-clickable">
              <thead>
                <tr>
                  <th>Имя</th>
                  <th>Адрес</th>
                  <th>Статус</th>
                  <th>Сессии</th>
                  <th>Последняя проверка</th>
                  <th className="col-actions" />
                </tr>
              </thead>
              <tbody>
                {(nodes ?? []).map((n) => (
                  <tr
                    key={n.id}
                    className="row-click"
                    onClick={() => navigate(`/nodes/${n.id}`)}
                  >
                    <td>
                      <Link
                        to={`/nodes/${n.id}`}
                        className="node-name-link"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {n.name}
                      </Link>
                    </td>
                    <td className="mono">{n.url}</td>
                    <td>
                      <StatusBadge status={n.status} large />
                    </td>
                    <td className="mono">{statsMap[n.id] ?? '…'}</td>
                    <td className="muted">{formatLastSeen(n.last_seen)}</td>
                    <td
                      className="col-actions"
                      onClick={(e) => e.stopPropagation()}
                    >
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
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

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
    try {
      await navigator.clipboard.writeText(text)
      setCopied(label)
      setTimeout(() => setCopied(''), 1500)
    } catch {
      setError('Буфер обмена недоступен')
    }
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
                Клиенты — 443 (HAProxy). Панель ходит к агенту напрямую по HTTP
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
