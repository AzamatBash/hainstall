import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, translateError, type ProxyEngine, type ProxyProfile } from '../api'

function fmt(ts?: string): string {
  if (!ts) return '—'
  try {
    return new Date(ts).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })
  } catch {
    return ts
  }
}

export default function ProxyProfilesPage() {
  const [profiles, setProfiles] = useState<ProxyProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [name, setName] = useState('')
  const [engine, setEngine] = useState<ProxyEngine>('haproxy')
  const [body, setBody] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)

  const selected = useMemo(
    () => profiles.find((p) => p.id === selectedId) || null,
    [profiles, selectedId],
  )

  const load = useCallback(async () => {
    setError('')
    try {
      const res = await api<{ profiles?: ProxyProfile[] }>('/api/proxy-profiles')
      const list = Array.isArray(res.profiles) ? res.profiles : []
      setProfiles(list)
      setSelectedId((prev) => {
        if (prev && list.some((p) => p.id === prev)) return prev
        return list[0]?.id || ''
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!selected) {
      setName('')
      setEngine('haproxy')
      setBody('')
      setNote('')
      return
    }
    setName(selected.name)
    setEngine(selected.engine)
    setBody(selected.body)
    setNote(selected.note || '')
  }, [selected])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api<{ profile: ProxyProfile }>('/api/proxy-profiles', {
        method: 'POST',
        body: JSON.stringify({
          name: name.trim() || (engine === 'envoy' ? 'envoy-profile' : 'haproxy-profile'),
          engine,
        }),
      })
      setCreating(false)
      await load()
      setSelectedId(res.profile.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setBusy(false)
    }
  }

  async function onSave() {
    if (!selected) return
    setBusy(true)
    setError('')
    try {
      const res = await api<{ profile: ProxyProfile }>(`/api/proxy-profiles/${selected.id}`, {
        method: 'PUT',
        body: JSON.stringify({ name, engine, body, note }),
      })
      await load()
      setSelectedId(res.profile.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setBusy(false)
    }
  }

  async function onDelete() {
    if (!selected) return
    if (!confirm(`Удалить профиль «${selected.name}»? С нод только снимется назначение в панели.`)) {
      return
    }
    setBusy(true)
    setError('')
    try {
      await api(`/api/proxy-profiles/${selected.id}`, { method: 'DELETE' })
      setSelectedId('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="shell">
      <header className="page-toolbar">
        <div className="page-toolbar-title">
          <h1>Профили</h1>
          <p className="muted">
            Здесь заполняете шаблоны конфига (HAProxy или Envoy). Потом на{' '}
            <Link to="/nodes">ноде</Link> выбираете нужный шаблон. Бэкенды по-прежнему
            добавляются на ноде как раньше. На агент пока не уходит — только хранение в панели.
          </p>
        </div>
        <div className="page-toolbar-actions">
          <button
            className="btn btn-primary"
            type="button"
            disabled={busy}
            onClick={() => {
              setCreating(true)
              setSelectedId('')
              setName('')
              setEngine('haproxy')
              setBody('')
              setNote('')
            }}
          >
            Новый профиль
          </button>
        </div>
      </header>

      {error && <p className="error">{error}</p>}

      <div className="row" style={{ gap: '1rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
        <section className="panel" style={{ flex: '0 0 16rem', minWidth: '14rem' }}>
          <h2 style={{ marginTop: 0 }}>Список</h2>
          {loading && <p className="muted">Загрузка…</p>}
          {!loading && profiles.length === 0 && !creating && (
            <p className="muted">Профилей пока нет.</p>
          )}
          <div className="stack" style={{ gap: '0.35rem' }}>
            {profiles.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`btn btn-sm${p.id === selectedId && !creating ? '' : ' btn-ghost'}`}
                style={{ justifyContent: 'flex-start', textAlign: 'left' }}
                onClick={() => {
                  setCreating(false)
                  setSelectedId(p.id)
                }}
              >
                <span>
                  {p.name}
                  <span className="muted" style={{ display: 'block', fontSize: '0.75rem' }}>
                    {p.engine} · rev {p.revision}
                  </span>
                </span>
              </button>
            ))}
          </div>
        </section>

        <section className="panel" style={{ flex: '1 1 28rem', minWidth: '18rem' }}>
          {creating ? (
            <>
              <h2 style={{ marginTop: 0 }}>Новый профиль</h2>
              <form className="stack" onSubmit={(e) => void onCreate(e)}>
                <div className="field">
                  <label htmlFor="pp-name">Название</label>
                  <input
                    id="pp-name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="haproxy-default"
                    maxLength={80}
                    required
                  />
                </div>
                <div className="field">
                  <label htmlFor="pp-engine">Движок</label>
                  <select
                    id="pp-engine"
                    value={engine}
                    onChange={(e) => setEngine(e.target.value as ProxyEngine)}
                  >
                    <option value="haproxy">HAProxy</option>
                    <option value="envoy">Envoy</option>
                  </select>
                </div>
                <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
                  Стартовый шаблон подставится автоматически. Envoy пока только хранение в панели.
                </p>
                <div className="row">
                  <button className="btn btn-primary" type="submit" disabled={busy}>
                    Создать
                  </button>
                  <button
                    className="btn btn-ghost"
                    type="button"
                    disabled={busy}
                    onClick={() => setCreating(false)}
                  >
                    Отмена
                  </button>
                </div>
              </form>
            </>
          ) : selected ? (
            <>
              <div className="panel-head">
                <h2 style={{ marginTop: 0 }}>Редактор</h2>
                <div className="panel-head-aside muted" style={{ fontSize: '0.85rem' }}>
                  rev {selected.revision} · обновлён {fmt(selected.updated_at)}
                </div>
              </div>
              <div className="stack">
                <div className="row" style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
                  <div className="field" style={{ flex: '1 1 12rem' }}>
                    <label htmlFor="pp-edit-name">Название</label>
                    <input
                      id="pp-edit-name"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      maxLength={80}
                    />
                  </div>
                  <div className="field" style={{ flex: '0 0 10rem' }}>
                    <label htmlFor="pp-edit-engine">Движок</label>
                    <select
                      id="pp-edit-engine"
                      value={engine}
                      onChange={(e) => setEngine(e.target.value as ProxyEngine)}
                    >
                      <option value="haproxy">HAProxy</option>
                      <option value="envoy">Envoy</option>
                    </select>
                  </div>
                </div>
                <div className="field">
                  <label htmlFor="pp-note">Заметка</label>
                  <input
                    id="pp-note"
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                    placeholder="опционально"
                  />
                </div>
                <div className="field">
                  <label htmlFor="pp-body">Конфиг</label>
                  <textarea
                    id="pp-body"
                    className="mono"
                    value={body}
                    onChange={(e) => setBody(e.target.value)}
                    rows={22}
                    spellCheck={false}
                    style={{ width: '100%', minHeight: '22rem', fontSize: '0.85rem' }}
                  />
                </div>
                <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
                  Сохранение с изменением текста поднимает revision. На ноды это само не уходит.
                </p>
                <div className="row">
                  <button className="btn btn-primary" type="button" disabled={busy} onClick={() => void onSave()}>
                    Сохранить
                  </button>
                  <button className="btn btn-ghost danger" type="button" disabled={busy} onClick={() => void onDelete()}>
                    Удалить
                  </button>
                </div>
              </div>
            </>
          ) : (
            <p className="muted">Выберите профиль или создайте новый.</p>
          )}
        </section>
      </div>
    </div>
  )
}
