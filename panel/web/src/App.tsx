import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { api, setToken } from './api'
import BrandNav from './components/BrandNav'
import LoginPage from './pages/LoginPage'
import NodesPage from './pages/NodesPage'
import NodeDetailPage from './pages/NodeDetailPage'
import TasksPage from './pages/TasksPage'
import StatsPage from './pages/StatsPage'
import OlcrtcPage from './pages/OlcrtcPage'

function PageFade() {
  const { pathname } = useLocation()
  useEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])
  return (
    <div className="page-fade" key={pathname}>
      <Outlet />
    </div>
  )
}

function AuthedLayout() {
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
      <div className="shell page-fade">
        <header className="topbar">
          <BrandNav active="panel" />
        </header>
        <p className="muted">Проверка сессии…</p>
      </div>
    )
  }
  if (!ok) return <Navigate to="/login" replace />
  return <PageFade />
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthedLayout />}>
        <Route path="/" element={<NodesPage />} />
        <Route path="/nodes/:id" element={<NodeDetailPage />} />
        <Route path="/tasks" element={<TasksPage />} />
        <Route path="/stats" element={<StatsPage />} />
        <Route path="/olcnode" element={<OlcrtcPage />} />
      </Route>
      <Route path="/olcrtc" element={<Navigate to="/olcnode" replace />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
