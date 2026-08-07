import { FormEvent, useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { api, getToken, setToken } from '../api'
import { withBase } from '../basePath'

export default function LoginPage() {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(true)
  const [alreadyIn, setAlreadyIn] = useState(!!getToken())

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        await api('/api/auth/me')
        if (!cancelled) setAlreadyIn(true)
      } catch {
        if (!cancelled) setAlreadyIn(false)
      } finally {
        if (!cancelled) setChecking(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  if (checking) {
    return (
      <div className="login-wrap">
        <p className="muted">Проверка сессии…</p>
      </div>
    )
  }

  if (alreadyIn) {
    return <Navigate to="/" replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api<{ token: string }>('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify({ password }),
      })
      setToken(res.token)
      window.location.href = withBase('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Ошибка входа')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card stack" onSubmit={onSubmit}>
        <div>
          <h1>
            ha<span style={{ color: 'var(--accent)' }}>panel</span>
          </h1>
          <p className="muted">Войдите, чтобы управлять нодами HAProxy</p>
        </div>
        <div className="field">
          <label htmlFor="password">Пароль</label>
          <input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </div>
        {error && <p className="error">{error}</p>}
        <button className="btn btn-primary" type="submit" disabled={busy}>
          {busy ? 'Вход…' : 'Войти'}
        </button>
      </form>
    </div>
  )
}
