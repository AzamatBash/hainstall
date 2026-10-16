import { FormEvent, useCallback, useEffect, useState } from 'react'
import { api, setToken, translateError } from '../api'
import { withBase } from '../basePath'
import { copyToClipboard } from '../clipboard'
import BrandNav from '../components/BrandNav'

interface ApiKeyRow {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at?: string
  revoked?: boolean
  revoked_at?: string
  key?: string
}

function fmt(ts?: string): string {
  if (!ts) return '—'
  try {
    return new Date(ts).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })
  } catch {
    return ts
  }
}

export default function ApiKeysPage() {
  const [keys, setKeys] = useState<ApiKeyRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [name, setName] = useState('export')
  const [busy, setBusy] = useState(false)
  const [fresh, setFresh] = useState<ApiKeyRow | null>(null)
  const [copied, setCopied] = useState(false)

  const load = useCallback(async () => {
    setError('')
    try {
      const res = await api<{ keys?: ApiKeyRow[] }>('/api/settings/api-keys')
      setKeys(Array.isArray(res.keys) ? res.keys : [])
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function logout() {
    try {
      await api('/api/auth/logout', { method: 'POST' })
    } catch {
      /* ignore */
    }
    setToken(null)
    window.location.href = withBase('/login')
  }

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    setCopied(false)
    try {
      const res = await api<{ key: ApiKeyRow }>('/api/settings/api-keys', {
        method: 'POST',
        body: JSON.stringify({ name: name.trim() || 'export' }),
      })
      setFresh(res.key)
      setName('export')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setBusy(false)
    }
  }

  async function onRevoke(id: string, label: string) {
    if (!confirm(`Отозвать ключ «${label}»? Внешняя панель перестанет получать статистику.`)) return
    try {
      await api(`/api/settings/api-keys/${id}/revoke`, { method: 'POST' })
      if (fresh?.id === id) setFresh(null)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    }
  }

  async function onDelete(id: string, label: string) {
    if (!confirm(`Удалить ключ «${label}» из списка?`)) return
    try {
      await api(`/api/settings/api-keys/${id}`, { method: 'DELETE' })
      if (fresh?.id === id) setFresh(null)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    }
  }

  async function copyKey() {
    if (!fresh?.key) return
    const ok = await copyToClipboard(fresh.key)
    setCopied(ok)
  }

  return (
    <div className="shell">
      <header className="topbar">
        <BrandNav active="api" />
        <div className="row">
          <button className="btn btn-sm btn-ghost" type="button" onClick={() => void logout()}>
            Выйти
          </button>
        </div>
      </header>

      <div className="team-head">
        <div>
          <h1 className="team-title">API ключи</h1>
          <p className="muted" style={{ margin: '0.2rem 0 0' }}>
            Ключ для внешней панели: <code>GET /api/v1/hanodes</code> (CPU и трафик ханод).
            Заголовок <code>X-API-Key</code> или <code>Authorization: Bearer …</code>.
          </p>
        </div>
        <form className="row" onSubmit={(e) => void onCreate(e)}>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Название"
            maxLength={64}
            aria-label="Название ключа"
          />
          <button className="btn btn-primary" type="submit" disabled={busy}>
            Сгенерировать
          </button>
        </form>
      </div>

      {error && <p className="error">{error}</p>}

      {fresh?.key && (
        <div className="api-key-fresh" role="status">
          <p>
            <strong>Новый ключ</strong> — скопируйте сейчас, повторно показать нельзя.
          </p>
          <code className="api-key-secret">{fresh.key}</code>
          <div className="row" style={{ marginTop: '0.75rem' }}>
            <button className="btn btn-sm btn-primary" type="button" onClick={() => void copyKey()}>
              {copied ? 'Скопировано' : 'Копировать'}
            </button>
            <button className="btn btn-sm" type="button" onClick={() => setFresh(null)}>
              Скрыть
            </button>
          </div>
        </div>
      )}

      {loading && <p className="muted">Загрузка…</p>}

      {!loading && keys.length === 0 && (
        <p className="muted">Ключей пока нет — нажмите «Сгенерировать».</p>
      )}

      {!loading && keys.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Название</th>
                <th>Префикс</th>
                <th>Создан</th>
                <th>Последний раз</th>
                <th>Статус</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td>{k.name}</td>
                  <td>
                    <code>{k.prefix}…</code>
                  </td>
                  <td className="muted">{fmt(k.created_at)}</td>
                  <td className="muted">{fmt(k.last_used_at)}</td>
                  <td>
                    {k.revoked ? (
                      <span className="badge offline">отозван</span>
                    ) : (
                      <span className="badge online">активен</span>
                    )}
                  </td>
                  <td>
                    <div className="row" style={{ justifyContent: 'flex-end' }}>
                      {!k.revoked && (
                        <button
                          className="btn btn-sm"
                          type="button"
                          onClick={() => void onRevoke(k.id, k.name)}
                        >
                          Отозвать
                        </button>
                      )}
                      <button
                        className="btn btn-sm btn-ghost"
                        type="button"
                        onClick={() => void onDelete(k.id, k.name)}
                      >
                        Удалить
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <section className="api-key-help">
        <h2>Пример</h2>
        <pre className="api-key-example">{`curl -sS -H "X-API-Key: <ключ>" \\
  "https://<панель>/api/v1/hanodes?hours=24"`}</pre>
      </section>
    </div>
  )
}
