import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'

export type MetricPoint = { t: number; value: number }

/** @deprecated Prefer MetricPoint; kept for online API mapping. */
export type OnlinePoint = { t: number; online: number }

type Props = {
  points: MetricPoint[]
  hours: number
  onZoom?: (from: number, to: number) => void
  className?: string
  valueLabel?: string
  formatValue?: (n: number) => string
  ariaLabel?: string
  emptyHint?: string
  tapHint?: string
}

type PointInfo = {
  x: number
  y: number
  date: string
  time: string
  value: number
  valueText: string
}

const TAP_SLOP = 14

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

function formatHover(ts: number, value: number, formatValue: (n: number) => string): Omit<PointInfo, 'x' | 'y'> {
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
  return { date, time: `${time} МСК`, value, valueText: formatValue(value) }
}

function downsample(points: MetricPoint[], maxPoints: number): MetricPoint[] {
  if (points.length <= maxPoints) return points
  const bucket = Math.ceil(points.length / maxPoints)
  const out: MetricPoint[] = []
  for (let i = 0; i < points.length; i += bucket) {
    const slice = points.slice(i, i + bucket)
    let min = slice[0]
    let max = slice[0]
    for (const p of slice) {
      if (p.value < min.value) min = p
      if (p.value > max.value) max = p
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

function Plaque({
  info,
  valueLabel,
  className,
}: {
  info: PointInfo
  valueLabel: string
  className?: string
}) {
  return (
    <div className={className} role="tooltip">
      <div className="online-chart-plaque-date">{info.date}</div>
      <div className="online-chart-plaque-time">{info.time}</div>
      <div className="online-chart-plaque-online">
        {valueLabel}: <span className="mono">{info.valueText}</span>
      </div>
    </div>
  )
}

function defaultFormat(n: number) {
  if (!Number.isFinite(n)) return '—'
  return String(Math.round(n))
}

export default function OnlineUsersChart({
  points,
  hours,
  onZoom,
  className,
  valueLabel = 'Онлайн',
  formatValue = defaultFormat,
  ariaLabel = 'Онлайн пользователей',
  emptyHint = 'Пока нет точек — подождите первый опрос (до 5 мин)',
  tapHint = 'Нажмите на график, чтобы увидеть дату и значение',
}: Props) {
  const width = 720
  const height = 260
  const padL = formatValue !== defaultFormat ? 58 : 44
  const padR = 12
  const padT = 18
  const padB = 30
  const [active, setActive] = useState<PointInfo | null>(null)
  const dragRef = useRef<{
    startX: number
    startT: number
    pointerId: number
    moved: boolean
  } | null>(null)
  const [dragX, setDragX] = useState<number | null>(null)

  const drawPoints = useMemo(() => downsample(points, hours >= 168 ? 800 : 1200), [points, hours])

  useEffect(() => {
    setActive(null)
  }, [points, hours])

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
    const maxVal = Math.max(1, ...drawPoints.map((p) => p.value))
    const niceMax = Math.max(1, maxVal * 1.1)
    const plotW = width - padL - padR
    const plotH = height - padT - padB
    const xAtFn = (t: number) => padL + ((t - t0) / span) * plotW
    const yAtFn = (v: number) => padT + plotH - (Math.min(v, niceMax) / niceMax) * plotH
    const tAtFn = (x: number) => t0 + ((x - padL) / plotW) * span
    const d = drawPoints
      .map((p, i) => `${i === 0 ? 'M' : 'L'}${xAtFn(p.t).toFixed(1)},${yAtFn(p.value).toFixed(1)}`)
      .join(' ')
    const areaD = `${d} L${xAtFn(t1).toFixed(1)},${(padT + plotH).toFixed(1)} L${xAtFn(t0).toFixed(1)},${(padT + plotH).toFixed(1)} Z`
    const labels: { x: number; label: string }[] = []
    const n = hours <= 1 ? 3 : hours <= 24 ? 4 : 5
    for (let i = 0; i < n; i++) {
      const t = t0 + (span * i) / Math.max(n - 1, 1)
      labels.push({ x: xAtFn(t), label: formatTick(t, hours) })
    }
    return { path: d, area: areaD, maxY: niceMax, xLabels: labels, xAt: xAtFn, yAt: yAtFn, tAt: tAtFn }
  }, [drawPoints, hours, padL])

  function nearest(t: number): MetricPoint | null {
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

  function infoAtX(svgX: number): PointInfo | null {
    const p = nearest(tAt(svgX))
    if (!p) return null
    return { x: xAt(p.t), y: yAt(p.value), ...formatHover(p.t, p.value, formatValue) }
  }

  function svgXFromEvent(e: ReactPointerEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    return ((e.clientX - rect.left) / rect.width) * width
  }

  function onMove(e: ReactPointerEvent<SVGSVGElement>) {
    const x = svgXFromEvent(e)
    const info = infoAtX(x)
    if (dragRef.current && dragRef.current.pointerId === e.pointerId) {
      if (Math.abs(x - dragRef.current.startX) > TAP_SLOP) {
        dragRef.current.moved = true
      }
      if (dragRef.current.moved) {
        setDragX(x)
        return
      }
    }
    if (e.pointerType === 'mouse' && info) setActive(info)
  }

  function onDown(e: ReactPointerEvent<SVGSVGElement>) {
    const x = svgXFromEvent(e)
    dragRef.current = {
      startX: x,
      startT: tAt(x),
      pointerId: e.pointerId,
      moved: false,
    }
    setDragX(null)
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  function onUp(e: ReactPointerEvent<SVGSVGElement>) {
    const drag = dragRef.current
    dragRef.current = null
    setDragX(null)
    if (!drag || drag.pointerId !== e.pointerId) return
    const x = svgXFromEvent(e)
    if (!drag.moved) {
      const info = infoAtX(x)
      if (info) setActive(info)
      return
    }
    if (!onZoom) return
    const from = Math.min(drag.startT, tAt(x))
    const to = Math.max(drag.startT, tAt(x))
    if (to - from < 60_000) return
    onZoom(from, to)
  }

  const yTicks = [maxY, maxY / 2, 0]
  const dragBand =
    dragRef.current?.moved && dragX != null
      ? {
          x: Math.min(dragRef.current.startX, dragX),
          w: Math.abs(dragX - dragRef.current.startX),
        }
      : null

  const tipLeftPct = active ? (active.x / width) * 100 : 0
  const tipTopPct = active ? (active.y / height) * 100 : 0
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
          aria-label={ariaLabel}
          onPointerMove={onMove}
          onPointerLeave={(e) => {
            if (e.pointerType === 'mouse') {
              if (!dragRef.current) setActive(null)
            }
            dragRef.current = null
            setDragX(null)
          }}
          onPointerDown={onDown}
          onPointerUp={onUp}
          onPointerCancel={() => {
            dragRef.current = null
            setDragX(null)
          }}
        >
          {yTicks.map((v, i) => {
            const y = yAt(v)
            return (
              <g key={`y-${i}-${v}`}>
                <line x1={padL} x2={width - padR} y1={y} y2={y} className="traffic-grid" />
                <text x={padL - 6} y={y + 3} textAnchor="end" className="traffic-axis">
                  {formatValue(v)}
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
              {emptyHint}
            </text>
          ) : null}
          {active ? (
            <>
              <line
                x1={active.x}
                x2={active.x}
                y1={padT}
                y2={height - padB}
                className="online-chart-cross"
              />
              <circle cx={active.x} cy={active.y} r={4.5} className="online-chart-dot" />
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

        {active && !dragRef.current?.moved ? (
          <div
            className={`online-chart-plaque online-chart-plaque-float${tipFlipX ? ' flip-x' : ''}${tipFlipY ? ' flip-y' : ''}`}
            style={{ left: `${tipLeftPct}%`, top: `${tipTopPct}%` }}
            role="tooltip"
          >
            <div className="online-chart-plaque-date">{active.date}</div>
            <div className="online-chart-plaque-time">{active.time}</div>
            <div className="online-chart-plaque-online">
              {valueLabel}: <span className="mono">{active.valueText}</span>
            </div>
          </div>
        ) : null}
      </div>

      {active ? (
        <Plaque info={active} valueLabel={valueLabel} className="online-chart-plaque online-chart-plaque-dock" />
      ) : (
        <p className="online-chart-tap-hint muted">{tapHint}</p>
      )}
    </div>
  )
}
