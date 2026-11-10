import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, translateError, type ProxyEngine, type ProxyProfile } from '../api'

type Validation = {
  ok: boolean
  engine?: string
  method?: string
  message?: string
  detail?: string
}

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
  const [validation, setValidation] = useState<Validation | null>(null)

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

  const loadDefaultBody = useCallback(async (eng: ProxyEngine) => {
    const res = await api<{ body?: string }>(`/api/proxy-profiles/defaults?engine=${eng}`)
    setBody(typeof res.body === 'string' ? res.body : '')
    setValidation(null)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (creating) return
    if (!selected) {
      setName('')
      setEngine('haproxy')
      setBody('')
      setNote('')
      setValidation(null)
      return
    }
    setName(selected.name)
    setEngine(selected.engine)
    setBody(selected.body)
    setNote(selected.note || '')
    setValidation(null)
  }, [selected, creating])

  async function startCreate() {
    setCreating(true)
    setSelectedId('')
    setName('vpn-standard')
    setEngine('haproxy')
    setNote('Стандартный VPN-шаблон hapanel')
    setValidation(null)
    setError('')
    try {
      await loadDefaultBody('haproxy')
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    }
  }

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    setValidation(null)
    try {
      const res = await api<{ profile: ProxyProfile; validation?: Validation }>('/api/proxy-profiles', {
        method: 'POST',
        body: JSON.stringify({
          name: name.trim() || (engine === 'envoy' ? 'envoy-profile' : 'vpn-standard'),
          engine,
          body,
          note,
        }),
      })
      if (res.validation) setValidation(res.validation)
      setCreating(false)
      await load()
      setSelectedId(res.profile.id)
    } catch (err) {
      const msg = err instanceof Error ? err.message : translateError(String(err))
      setError(msg)
      setValidation({ ok: false, message: msg })
    } finally {
      setBusy(false)
    }
  }

  async function onSave() {
    if (!selected) return
    setBusy(true)
    setError('')
    setValidation(null)
    try {
      const res = await api<{ profile: ProxyProfile; validation?: Validation }>(
        `/api/proxy-profiles/${selected.id}`,
        {
          method: 'PUT',
          body: JSON.stringify({ name, engine, body, note }),
        },
      )
      if (res.validation) setValidation(res.validation)
      await load()
      setSelectedId(res.profile.id)
    } catch (err) {
      const msg = err instanceof Error ? err.message : translateError(String(err))
      setError(msg)
      setValidation({ ok: false, message: msg })
    } finally {
      setBusy(false)
    }
  }

  async function onValidate() {
    setBusy(true)
    setError('')
    try {
      const res = await api<Validation>('/api/proxy-profiles/validate', {
        method: 'POST',
        body: JSON.stringify({ engine, body }),
      })
      setValidation(res)
    } catch (err) {
      // 422 returns ApiError — try to show message; also fetch raw if needed
      const msg = err instanceof Error ? err.message : translateError(String(err))
      setValidation({ ok: false, message: msg })
      setError(msg)
    } finally {
      setBusy(false)
    }
  }

  async function onResetTemplate() {
    if (!confirm('Подставить стандартный VPN-шаблон? Текущий текст в редакторе будет заменён.')) {
      return
    }
    setBusy(true)
    setError('')
    try {
      await loadDefaultBody(engine)
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

  const editor = (
    <div className="stack">
      <div className="row" style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
        <div className="field" style={{ flex: '1 1 12rem' }}>
          <label htmlFor="pp-name">Название</label>
          <input
            id="pp-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="vpn-standard"
            maxLength={80}
            required={creating}
          />
        </div>
        <div className="field" style={{ flex: '0 0 10rem' }}>
          <label htmlFor="pp-engine">Движок</label>
          <select
            id="pp-engine"
            value={engine}
            onChange={(e) => {
              const next = e.target.value as ProxyEngine
              setEngine(next)
              if (creating) void loadDefaultBody(next)
            }}
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
          onChange={(e) => {
            setBody(e.target.value)
            setValidation(null)
          }}
          rows={24}
          spellCheck={false}
          style={{ width: '100%', minHeight: '24rem', fontSize: '0.85rem' }}
        />
      </div>
      {validation && (
        <p className={validation.ok ? 'muted' : 'error'} style={{ margin: 0, fontSize: '0.9rem' }}>
          {validation.ok ? '✓' : '✗'} {validation.message}
          {validation.method ? ` · ${validation.method}` : ''}
          {validation.detail ? (
            <span className="mono" style={{ display: 'block', whiteSpace: 'pre-wrap', marginTop: '0.35rem' }}>
              {validation.detail}
            </span>
          ) : null}
        </p>
      )}
      <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
        Стандартный шаблон = как на нодах сейчас (maxconn, ClientHello, rate-limit). Бэкенды
        добавляются на ноде. Сохранение без валидного конфига отклоняется.
      </p>
      <div className="row" style={{ flexWrap: 'wrap' }}>
        {creating ? (
          <button className="btn btn-primary" type="submit" disabled={busy}>
            Создать
          </button>
        ) : (
          <button className="btn btn-primary" type="button" disabled={busy} onClick={() => void onSave()}>
            Сохранить
          </button>
        )}
        <button className="btn btn-ghost" type="button" disabled={busy} onClick={() => void onValidate()}>
          Проверить конфиг
        </button>
        <button className="btn btn-ghost" type="button" disabled={busy} onClick={() => void onResetTemplate()}>
          Стандартный шаблон
        </button>
        {creating ? (
          <button className="btn btn-ghost" type="button" disabled={busy} onClick={() => setCreating(false)}>
            Отмена
          </button>
        ) : (
          <button className="btn btn-ghost danger" type="button" disabled={busy} onClick={() => void onDelete()}>
            Удалить
          </button>
        )}
      </div>
    </div>
  )

  return (
    <div className="shell">
      <header className="page-toolbar">
        <div className="page-toolbar-title">
          <h1>Профили</h1>
          <p className="muted">
            Стандартный VPN-шаблон уже заполнен. На{' '}
            <Link to="/nodes">ноде</Link> выбираете шаблон; бэкенды — как раньше.
          </p>
        </div>
        <div className="page-toolbar-actions">
          <button className="btn btn-primary" type="button" disabled={busy} onClick={() => void startCreate()}>
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
            <p className="muted">Профилей пока нет — создайте стандартный.</p>
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
                {editor}
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
              {editor}
            </>
          ) : (
            <p className="muted">Выберите профиль или создайте стандартный VPN-шаблон.</p>
          )}
        </section>
      </div>
    </div>
  )
}
