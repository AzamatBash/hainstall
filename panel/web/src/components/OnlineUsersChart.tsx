import { useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'

export type OnlinePoint = { t: number; online: number }

type Props = {
  points: OnlinePoint[]
  hours: number
  onZoom?: (from: number, to: number) => void
  className?: string
}

type HoverInfo = {
  x: number
  y: number
  date: string
  time: string
  online: number
}

function formatTick(ts: number, hours: number) {
  const d = new Date(ts)
  const opts: Intl.DateTimeFormatOptions = { timeZone: 'Europe/Moscow' }
  if (hours <= 24) {
    return d.toLocaleTimeString('ru-RU', { ...opts, hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleString('ru-RU', {
    ...opts,
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatHover(ts: number, online: number): Omit<HoverInfo, 'x' | 'y'> {
  const d = new Date(ts)
  const date = d.toLocaleDateString('ru-RU', {
    timeZone: 'Europe/Moscow',
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
  })
  const time = d.toLocaleTimeString('ru-RU', {
    timeZone: 'Europe/Moscow',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  return { date, time: `${time} МСК`, online }
}

/** Downsample for draw while keeping peaks (min/max per bucket). */
function downsample(points: OnlinePoint[], maxPoints: number): OnlinePoint[] {
  if (points.length <= maxPoints) return points
  const bucket = Math.ceil(points.length / maxPoints)
  const out: OnlinePoint[] = []
  for (let i = 0; i < points.length; i += bucket) {
    const slice = points.slice(i, i + bucket)
    let min = slice[0]
    let max = slice[0]
    for (const p of slice) {
      if (p.online < min.online) min = p
      if (p.online > max.online) max = p
    }
    if (min.t <= max.t) {
      out.push(min)
      if (max.t !== min.t) out.push(max)
    } else {
      out.push(max)
      if (min.t !== max.t) out.push(min)
    }
  }
  return out
}

export default function OnlineUsersChart({ points, hours, onZoom, className }: Props) {
  const width = 720
  const height = 240
  const padL = 48
  const padR = 14
  const padT = 16
  const padB = 28
  const [hover, setHover] = useState<HoverInfo | null>(null)
  const dragRef = useRef<{ startX: number; startT: number } | null>(null)
  const [dragX, setDragX] = useState<number | null>(null)

  const drawPoints = useMemo(() => downsample(points, hours >= 168 ? 800 : 1200), [points, hours])

  const { path, area, maxY, xLabels, xAt, yAt, tAt } = useMemo(() => {
    const empty = {
      path: '',
      area: '',
      maxY: 1,
      xLabels: [] as { x: number; label: string }[],
      xAt: (_t: number) => padL,
      yAt: (_v: number) => padT,
      tAt: (_x: number) => 0,
    }
    if (!drawPoints.length) return empty
    const t0 = drawPoints[0].t
    const t1 = drawPoints[drawPoints.length - 1].t
    const span = Math.max(t1 - t0, 1)
    const maxOnline = Math.max(1, ...drawPoints.map((p) => p.online))
    const niceMax = Math.max(1, Math.ceil(maxOnline * 1.1))
    const plotW = width - padL - padR
    const plotH = height - padT - padB
    const xAtFn = (t: number) => padL + ((t - t0) / span) * plotW
    const yAtFn = (v: number) => padT + plotH - (Math.min(v, niceMax) / niceMax) * plotH
    const tAtFn = (x: number) => t0 + ((x - padL) / plotW) * span
    const d = drawPoints
      .map((p, i) => `${i === 0 ? 'M' : 'L'}${xAtFn(p.t).toFixed(1)},${yAtFn(p.online).toFixed(1)}`)
      .join(' ')
    const areaD = `${d} L${xAtFn(t1).toFixed(1)},${(padT + plotH).toFixed(1)} L${xAtFn(t0).toFixed(1)},${(padT + plotH).toFixed(1)} Z`
    const labels: { x: number; label: string }[] = []
    const n = hours <= 1 ? 4 : hours <= 24 ? 6 : 7
    for (let i = 0; i < n; i++) {
      const t = t0 + (span * i) / Math.max(n - 1, 1)
      labels.push({ x: xAtFn(t), label: formatTick(t, hours) })
    }
    return { path: d, area: areaD, maxY: niceMax, xLabels: labels, xAt: xAtFn, yAt: yAtFn, tAt: tAtFn }
  }, [drawPoints, hours])

  function nearest(t: number): OnlinePoint | null {
    if (!points.length) return null
    let best = points[0]
    let bestDist = Math.abs(points[0].t - t)
    for (const p of points) {
      const d = Math.abs(p.t - t)
      if (d < bestDist) {
        best = p
        bestDist = d
      }
    }
    return best
  }

  function onMove(e: ReactPointerEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const x = ((e.clientX - rect.left) / rect.width) * width
    const t = tAt(x)
    const p = nearest(t)
    if (!p) {
      setHover(null)
      return
    }
    setHover({
      x: xAt(p.t),
      y: yAt(p.online),
      ...formatHover(p.t, p.online),
    })
    if (dragRef.current) setDragX(x)
  }

  function onDown(e: ReactPointerEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const x = ((e.clientX - rect.left) / rect.width) * width
    dragRef.current = { startX: x, startT: tAt(x) }
    setDragX(x)
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  function onUp(e: ReactPointerEvent<SVGSVGElement>) {
    const drag = dragRef.current
    dragRef.current = null
    setDragX(null)
    if (!drag || !onZoom) return
    const rect = e.currentTarget.getBoundingClientRect()
    const x = ((e.clientX - rect.left) / rect.width) * width
    const t1 = drag.startT
    const t2 = tAt(x)
    const from = Math.min(t1, t2)
    const to = Math.max(t1, t2)
    if (to - from < 60_000) return
    onZoom(from, to)
  }

  const yTicks = [maxY, Math.round(maxY / 2), 0]
  const dragBand =
    dragRef.current && dragX != null
      ? {
          x: Math.min(dragRef.current.startX, dragX),
          w: Math.abs(dragX - dragRef.current.startX),
        }
      : null

  const tipLeftPct = hover ? (hover.x / width) * 100 : 0
  const tipTopPct = hover ? (hover.y / height) * 100 : 0
  const tipFlipX = tipLeftPct > 62
  const tipFlipY = tipTopPct < 28

  return (
    <div className={`online-chart${className ? ` ${className}` : ''}`}>
      <div className="online-chart-frame">
        <svg
          className="online-chart-svg"
          viewBox={`0 0 ${width} ${height}`}
          preserveAspectRatio="none"
          role="img"
          aria-label="Онлайн пользователей"
          onPointerMove={onMove}
          onPointerLeave={() => {
            setHover(null)
            dragRef.current = null
            setDragX(null)
          }}
          onPointerDown={onDown}
          onPointerUp={onUp}
        >
          {yTicks.map((v) => {
            const y = yAt(v)
            return (
              <g key={`y-${v}`}>
                <line x1={padL} x2={width - padR} y1={y} y2={y} className="traffic-grid" />
                <text x={padL - 6} y={y + 3} textAnchor="end" className="traffic-axis">
                  {v}
                </text>
              </g>
            )
          })}
          {dragBand && dragBand.w > 2 ? (
            <rect
              x={dragBand.x}
              y={padT}
              width={dragBand.w}
              height={height - padT - padB}
              className="online-chart-brush"
            />
          ) : null}
          {area ? <path d={area} className="online-chart-area" /> : null}
          {path ? <path d={path} className="online-chart-line" /> : null}
          {!points.length ? (
            <text x={width / 2} y={height / 2} textAnchor="middle" className="traffic-empty">
              Пока нет точек — подождите первый опрос (до 5 мин)
            </text>
          ) : null}
          {hover ? (
            <>
              <line
                x1={hover.x}
                x2={hover.x}
                y1={padT}
                y2={height - padB}
                className="online-chart-cross"
              />
              <circle cx={hover.x} cy={hover.y} r={4} className="online-chart-dot" />
            </>
          ) : null}
          {xLabels.map((l) => (
            <text
              key={l.label + l.x}
              x={l.x}
              y={height - 8}
              textAnchor="middle"
              className="traffic-axis"
            >
              {l.label}
            </text>
          ))}
        </svg>

        {hover && !dragRef.current ? (
          <div
            className={`online-chart-plaque${tipFlipX ? ' flip-x' : ''}${tipFlipY ? ' flip-y' : ''}`}
            style={{ left: `${tipLeftPct}%`, top: `${tipTopPct}%` }}
            role="tooltip"
          >
            <div className="online-chart-plaque-date">{hover.date}</div>
            <div className="online-chart-plaque-time">{hover.time}</div>
            <div className="online-chart-plaque-online">
              Онлайн: <span className="mono">{hover.online}</span>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  )
}
