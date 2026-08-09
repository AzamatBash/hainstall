import { Link } from 'react-router-dom'

export type BrandNavActive = 'panel' | 'tasks'

export default function BrandNav({ active }: { active: BrandNavActive }) {
  return (
    <nav className="brand-nav" aria-label="Разделы">
      <Link to="/" className={`brand${active === 'panel' ? ' active' : ''}`}>
        ha<span>panel</span>
      </Link>
      <Link
        to="/tasks"
        className={`brand brand-tasks${active === 'tasks' ? ' active' : ''}`}
      >
        ta<span>sks</span>
      </Link>
    </nav>
  )
}
