import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  api,
  BackendServer,
  flattenBackends,
  formatBytes,
  formatLastSeen,
  formatUptime,
  Node,
  StatsSummary,
  statusLabel,
  SystemMetrics,
  translateError,
  userFacingStats,
} from '../api'
import { copyToClipboard, downloadTextFile } from '../clipboard'
import SparklineChart, { ChartPoint } from '../components/SparklineChart'

const HISTORY_MAX = 90
const POLL_MS = 5000

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
  const [backends, setBackends] = useState<BackendServer[]>([])
  const histResetRef = useRef(id)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const [toast, setToast] = useState<{ kind: 'ok' | 'fail'; text: string } | null>(
    null,
  )
  const [showInstall, setShowInstall] = useState(false)
  const [installBundle, setInstallBundle] = useState<Bundle | null>(null)
  const [installFileKey, setInstallFileKey] = useState('docker-compose.yml')
  const [copied, setCopied] = useState('')

  const [form, setForm] = useState({
    backend: 'app',
    name: '',
    address: '',
    port: '8443',
    weight: '100',
  })

  const facing = useMemo(() => userFacingStats(stats), [stats])

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
      if (found.status !== 'online') {
        setStats(null)
        setSystem(null)
        setBackends([])
        return
      }
      const [statsRes, backendsRes, systemRes] = await Promise.all([
        api<StatsSummary>(`/api/nodes/${id}/stats`),
        api<unknown>(`/api/nodes/${id}/backends`),
        api<SystemMetrics>(`/api/nodes/${id}/system`).catch(() => null),
      ])
      setStats(statsRes)
      setBackends(flattenBackends(backendsRes))
      if (systemRes) {
        setSystem(systemRes)
        setCpuHist((h) => pushPoint(h, Number(systemRes.cpu_percent) || 0))
        setMemHist((h) => pushPoint(h, Number(systemRes.mem_percent) || 0))
      }
      const facingNow = userFacingStats(statsRes)
      if (facingNow.active_sessions !== null) {
        setSessHist((h) => pushPoint(h, facingNow.active_sessions ?? 0))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Ошибка загрузки')
    }
  }, [id])

  useEffect(() => {
    if (histResetRef.current !== id) {
      histResetRef.current = id
      setCpuHist([])
      setMemHist([])
      setSessHist([])
      setSystem(null)
    }
  }, [id])

  useEffect(() => {
    void load()
  }, [load])

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
          setCpuHist((h) => pushPoint(h, Number(systemRes.cpu_percent) || 0))
          setMemHist((h) => pushPoint(h, Number(systemRes.mem_percent) || 0))
          const facingNow = userFacingStats(statsRes)
          if (facingNow.active_sessions !== null) {
            setSessHist((h) => pushPoint(h, facingNow.active_sessions ?? 0))
          }
        } catch {
          /* ignore transient poll errors */
        }
      })()
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [id, node?.status])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), 4500)
    return () => clearTimeout(t)
  }, [toast])

  const title = useMemo(() => node?.name ?? 'Нода', [node])
  const st = node?.status || 'unknown'

  async function onConnect() {
    setConnecting(true)
    setMsg('')
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
        const text = 'Связь есть — нода онлайн'
        setMsg(text)
        setToast({ kind: 'ok', text })
        await load()
      } else {
        const text =
          'Нет связи: ' +
          (translateError(res.error || '') ||
            'нода недоступна — проверьте docker compose')
        setError(text)
        setToast({ kind: 'fail', text })
      }
    } catch (err) {
      const text =
        'Нет связи: ' +
        (err instanceof Error ? err.message : 'ошибка проверки связи')
      setError(text)
      setToast({ kind: 'fail', text })
    } finally {
      setConnecting(false)
    }
  }

  async function runAction(label: string, path: string, method = 'POST') {
    setBusy(true)
    setMsg('')
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
      setMsg(text)
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
    setMsg('')
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
      setMsg('Бэкенд добавлен')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось добавить бэкенд')
    } finally {
      setBusy(false)
    }
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

  async function onDeleteNode() {
    if (!node) return
    if (!confirm(`Удалить ноду «${node.name}»?`)) return
    try {
      await api(`/api/nodes/${id}`, { method: 'DELETE' })
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить')
    }
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
      <header className="topbar">
        <div>
          <Link to="/" className="muted">
            ← Ноды
          </Link>
          <div className="brand" style={{ marginTop: '0.35rem' }}>
            {title}
          </div>
          {node && <div className="mono muted">{node.url}</div>}
        </div>
        <div className="row">
          <button className="btn btn-sm" type="button" disabled={busy} onClick={() => void load()}>
            Обновить
          </button>
          <button
            className="btn btn-sm"
            type="button"
            disabled={busy || st !== 'online'}
            onClick={() => void runAction('Перезагрузка', `/api/nodes/${id}/haproxy/reload`)}
          >
            Перезагрузить HAProxy
          </button>
          <button
            className="btn btn-sm btn-danger"
            type="button"
            disabled={busy || st !== 'online'}
            onClick={() => {
              if (confirm('Перезапустить HAProxy на этой ноде?')) {
                void runAction('Рестарт', `/api/nodes/${id}/haproxy/restart`)
              }
            }}
          >
            Рестарт
          </button>
          <button
            className="btn btn-sm btn-danger"
            type="button"
            onClick={() => void onDeleteNode()}
          >
            Удалить ноду
          </button>
        </div>
      </header>

      {toast && (
        <div className={`toast toast-${toast.kind}`} role="status">
          {toast.text}
        </div>
      )}

      {node && (
        <div className={`status-hero ${st}`}>
          <StatusBadge status={st} large />
          <div className="status-hero-text" style={{ flex: 1 }}>
            <div className="status-hero-title">{statusLabel(st)}</div>
            <div className="muted">
              {st === 'online'
                ? 'Панель успешно связывается с агентом'
                : st === 'offline'
                  ? 'Агент не отвечает — проверьте сервер и docker compose'
                  : 'Нода ещё не проверялась — нажмите «Проверить связь»'}
            </div>
            <div className="muted" style={{ marginTop: '0.25rem' }}>
              Последняя проверка: {formatLastSeen(node.last_seen)}
            </div>
          </div>
          <button
            className="btn btn-primary btn-connect"
            type="button"
            disabled={connecting || busy}
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
      {msg && !error && (
        <div className="connect-result ok" style={{ marginBottom: '1rem' }}>
          <strong>{msg}</strong>
        </div>
      )}

      <div className="stack" style={{ gap: '1rem' }}>
        <section className="panel">
          <h2>Статистика</h2>
          <div className="stats-grid">
            <div className="stat">
              <div className="label">Статус</div>
              <div className="value">
                <StatusBadge status={st} />
              </div>
            </div>
            <div className="stat">
              <div className="label">Сессии (приложения)</div>
              <div className="value">
                {facing.active_sessions !== null ? facing.active_sessions : '—'}
              </div>
            </div>
            <div className="stat">
              <div className="label">Бэкенды UP</div>
              <div className="value">
                {facing.backends_up !== null ? facing.backends_up : '—'}
              </div>
            </div>
          </div>
        </section>

        {st === 'online' && (
          <section className="panel">
            <h2>Ресурсы</h2>
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
                  {system
                    ? `${system.mem_percent.toFixed(1)}%`
                    : '—'}
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
                  <div className="value">
                    {(system.disk_percent ?? 0).toFixed(1)}%
                  </div>
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
            </div>
          </section>
        )}

        <section className="panel">
          <h2>Бэкенды приложений</h2>
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
                        ? 'Пока нет серверов — добавьте ниже'
                        : 'Нет данных — сначала установите связь'}
                    </td>
                  </tr>
                ) : (
                  backends.map((b) => (
                    <tr key={`${b.backend}-${b.name}`}>
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
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="panel">
          <h3>Добавить бэкенд</h3>
          <form className="stack" onSubmit={onAddBackend}>
            <div className="row" style={{ alignItems: 'flex-end' }}>
              {(
                [
                  ['backend', 'Бэкенд'],
                  ['name', 'Имя сервера'],
                  ['address', 'Адрес'],
                  ['port', 'Порт'],
                  ['weight', 'Вес'],
                ] as const
              ).map(([key, label]) => (
                <div
                  className="field"
                  key={key}
                  style={{ minWidth: key === 'address' ? 160 : 100 }}
                >
                  <label htmlFor={key}>{label}</label>
                  <input
                    id={key}
                    value={form[key]}
                    onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
                    required
                    disabled={st !== 'online'}
                  />
                </div>
              ))}
              <button className="btn btn-primary" type="submit" disabled={busy || st !== 'online'}>
                Добавить
              </button>
            </div>
          </form>
        </section>
      </div>

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
