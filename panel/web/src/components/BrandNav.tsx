import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { api, setToken } from '../api'
import { withBase } from '../basePath'

export type BrandNavActive =
  | 'panel'
  | 'tasks'
  | 'stats'
  | 'olcnode'
  | 'team'
  | 'api'
  | 'kb'

type NavItem = {
  id: BrandNavActive
  to: string
  label: string
  icon: ReactNode
}

type NavGroup = { title: string; items: NavItem[] }

function IconGrid() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <rect x="3.5" y="3.5" width="7" height="7" rx="1.5" />
      <rect x="13.5" y="3.5" width="7" height="7" rx="1.5" />
      <rect x="3.5" y="13.5" width="7" height="7" rx="1.5" />
      <rect x="13.5" y="13.5" width="7" height="7" rx="1.5" />
    </svg>
  )
}

function IconChart() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <path d="M4 19V5" strokeLinecap="round" />
      <path d="M4 19h16" strokeLinecap="round" />
      <path d="M8 15v-4M12 15V8M16 15v-6" strokeLinecap="round" />
    </svg>
  )
}

function IconTasks() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <rect x="4" y="4" width="16" height="16" rx="2" />
      <path d="M8 9h8M8 12h8M8 15h5" strokeLinecap="round" />
    </svg>
  )
}

function IconBook() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <path d="M5 5.5A2.5 2.5 0 0 1 7.5 3H19v16H7.5A2.5 2.5 0 0 0 5 21.5V5.5Z" />
      <path d="M5 21.5A2.5 2.5 0 0 1 7.5 19H19" />
    </svg>
  )
}

function IconLink() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <path d="M10 14a4 4 0 0 0 5.66 0l2.12-2.12a4 4 0 0 0-5.66-5.66L11 7" strokeLinecap="round" />
      <path d="M14 10a4 4 0 0 0-5.66 0L6.22 12.12a4 4 0 1 0 5.66 5.66L13 17" strokeLinecap="round" />
    </svg>
  )
}

function IconUsers() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <circle cx="9" cy="8" r="3" />
      <circle cx="16.5" cy="9" r="2.5" />
      <path d="M3.5 19c.6-3 2.8-4.5 5.5-4.5S14 16 14.5 19" strokeLinecap="round" />
      <path d="M15 14.5c1.8.2 3.4 1.2 4 3.5" strokeLinecap="round" />
    </svg>
  )
}

function IconKey() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="1.75">
      <circle cx="8.5" cy="10.5" r="3.5" />
      <path d="M11.5 10.5H20v3h-2v2h-2v-2h-1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

const GROUPS: NavGroup[] = [
  {
    title: 'Обзор',
    items: [
      { id: 'panel', to: '/', label: 'Ноды', icon: <IconGrid /> },
      { id: 'stats', to: '/stats', label: 'Статистика', icon: <IconChart /> },
      { id: 'kb', to: '/kb', label: 'База знаний', icon: <IconBook /> },
    ],
  },
  {
    title: 'Управление',
    items: [
      { id: 'tasks', to: '/tasks', label: 'Задачи', icon: <IconTasks /> },
      { id: 'team', to: '/team', label: 'Команда', icon: <IconUsers /> },
      { id: 'api', to: '/api-keys', label: 'API', icon: <IconKey /> },
    ],
  },
  {
    title: 'Инструменты',
    items: [{ id: 'olcnode', to: '/olcnode', label: 'olcNode', icon: <IconLink /> }],
  },
]

function activeFromPath(pathname: string): BrandNavActive {
  if (pathname.startsWith('/kb')) return 'kb'
  if (pathname.startsWith('/tasks')) return 'tasks'
  if (pathname.startsWith('/stats')) return 'stats'
  if (pathname.startsWith('/olcnode') || pathname.startsWith('/olcrtc')) return 'olcnode'
  if (pathname.startsWith('/team')) return 'team'
  if (pathname.startsWith('/api-keys')) return 'api'
  return 'panel'
}

function logout() {
  void api('/api/auth/logout', { method: 'POST' }).catch(() => {})
  setToken(null)
  window.location.href = withBase('/login')
}

/** Sidebar navigation in Remnawave-inspired layout; keeps hapanel branding/tones. */
export default function BrandNav({ active }: { active?: BrandNavActive }) {
  const { pathname } = useLocation()
  const current = active ?? activeFromPath(pathname)

  return (
    <aside className="app-sidebar" aria-label="Разделы">
      <Link to="/" className="sidebar-brand">
        <span className="sidebar-brand-mark" aria-hidden />
        <span className="sidebar-brand-text">
          ha<span>panel</span>
        </span>
      </Link>

      <nav className="sidebar-nav">
        {GROUPS.map((group) => (
          <div key={group.title} className="sidebar-group">
            <div className="sidebar-group-title">{group.title}</div>
            <ul className="sidebar-list">
              {group.items.map((item) => (
                <li key={item.id}>
                  <Link
                    to={item.to}
                    className={`sidebar-link${current === item.id ? ' active' : ''}`}
                  >
                    <span className="sidebar-link-icon">{item.icon}</span>
                    <span>{item.label}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>

      <div className="sidebar-foot">
        <button className="sidebar-logout" type="button" onClick={() => void logout()}>
          Выйти
        </button>
      </div>
    </aside>
  )
}
