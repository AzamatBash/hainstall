import { Navigate, Route, Routes } from 'react-router-dom'
import { useEffect, useState, type ReactNode } from 'react'
import { api, setToken } from './api'
import BrandNav from './components/BrandNav'
import LoginPage from './pages/LoginPage'
import NodesPage from './pages/NodesPage'
import NodeDetailPage from './pages/NodeDetailPage'
import TasksPage from './pages/TasksPage'

function Authed({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false)
  const [ok, setOk] = useState(false)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        // Cookie session and/or Bearer token — always ask the server.
        await api('/api/auth/me')
        if (!cancelled) setOk(true)
      } catch {
        setToken(null)
        if (!cancelled) setOk(false)
      } finally {
        if (!cancelled) setReady(true)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  if (!ready) {
    return (
      <div className="shell">
        <header className="topbar">
          <BrandNav active="panel" />
        </header>
        <p className="muted">Проверка сессии…</p>
      </div>
    )
  }
  if (!ok) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <Authed>
            <NodesPage />
          </Authed>
        }
      />
      <Route
        path="/nodes/:id"
        element={
          <Authed>
            <NodeDetailPage />
          </Authed>
        }
      />
      <Route
        path="/tasks"
        element={
          <Authed>
            <TasksPage />
          </Authed>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
