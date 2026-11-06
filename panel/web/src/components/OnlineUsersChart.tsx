import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import { bucketBySpan, seriesPath } from './chartCurve'
import {
  DAY_MS,
  WEEK_MS,
  formatWeekWhen,
  nearestByTime,
  signedDelta,
  weekSlackMs,
} from './weekOverlay'

export type MetricPoint = { t: number; value: number }

/** @deprecated Prefer MetricPoint; kept for online API mapping. */
export type OnlinePoint = { t: number; online: number }

type Props = {
  points: MetricPoint[]
  hours: number
  onZoom?: (from: number, to: number) => void
  /** Right click on the nearest sample. */
  onContextPoint?: (at: { t: number; clientX: number; clientY: number }) => void
  className?: string
  valueLabel?: string
  formatValue?: (n: number) => string
  ariaLabel?: string
  emptyHint?: string
  tapHint?: string
  /** Current value for the compact legend (defaults to last point). */
  current?: number | null
  showLegend?: boolean
  /** Same span one week earlier, still on its original timestamps. */
  prior?: MetricPoint[]
  /** Same span two weeks earlier. */
  prior2?: MetricPoint[]
}

type PointInfo = {
  x: number
  y: number
  date: string
  time: string
  value: number
  valueText: string
  priorText?: string
  priorWhen?: string
  deltaText?: string
  prior2Text?: string
  prior2When?: string
  delta2Text?: string
}

const TAP_SLOP = 14
const NO_PRIOR: MetricPoint[] = []

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
      {info.priorText ? (
        <div className="online-chart-plaque-online prior">
          Неделю назад: <span className="mono">{info.priorText}</span>
          {info.priorWhen ? <span className="online-chart-plaque-prior-when"> {info.priorWhen}</span> : null}
          {info.deltaText ? (
            <span className="online-chart-plaque-prior-when"> · {info.deltaText}</span>
          ) : null}
        </div>
      ) : null}
      {info.prior2Text ? (
        <div className="online-chart-plaque-online prior prior2">
          Вчера: <span className="mono">{info.prior2Text}</span>
          {info.prior2When ? <span className="online-chart-plaque-prior-when"> {info.prior2When}</span> : null}
          {info.delta2Text ? (
            <span className="online-chart-plaque-prior-when"> · {info.delta2Text}</span>
          ) : null}
        </div>
      ) : null}
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
  onContextPoint,
  className,
  valueLabel = 'Онлайн',
  formatValue = defaultFormat,
  ariaLabel = 'Онлайн пользователей',
  emptyHint = 'Пока нет точек — подождите первый опрос (до 5 мин)',
  tapHint = 'Нажмите на график, чтобы увидеть дату и значение',
  current = null,
  showLegend = false,
  prior = NO_PRIOR,
  prior2 = NO_PRIOR,
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

  const drawPoints = useMemo(
    () =>
      bucketBySpan(points, (group, center) => {
        let sum = 0
        for (const p of group) sum += p.value
        return { t: center, value: sum / group.length }
      }),
    [points],
  )

  useEffect(() => {
    setActive(null)
  }, [points, hours, prior, prior2])

  const priorDrawn = useMemo(
    () =>
      bucketBySpan(
        prior.map((p) => ({ ...p, t: p.t + WEEK_MS })),
        (group, center) => {
          let sum = 0
          for (const p of group) sum += p.value
          return { t: center, value: sum / group.length }
        },
      ),
    [prior],
  )

  const prior2Drawn = useMemo(
    () =>
      bucketBySpan(
        prior2.map((p) => ({ ...p, t: p.t + DAY_MS })),
        (group, center) => {
          let sum = 0
          for (const p of group) sum += p.value
          return { t: center, value: sum / group.length }
        },
      ),
    [prior2],
  )

  const { path, priorPath, prior2Path, area, maxY, xLabels, xAt, yAt, tAt } = useMemo(() => {
    const empty = {
      path: '',
      priorPath: '',
      prior2Path: '',
      area: '',
      maxY: 1,
      xLabels: [] as { x: number; label: string }[],
      xAt: (_t: number) => padL,
      yAt: (_v: number) => padT,
      tAt: (_x: number) => 0,
    }
    if (!drawPoints.length) return empty
    const t0 = points[0]?.t ?? drawPoints[0].t
    const t1 = points[points.length - 1]?.t ?? drawPoints[drawPoints.length - 1].t
    const span = Math.max(t1 - t0, 1)
    const maxVal = Math.max(
      1,
      ...drawPoints.map((p) => p.value),
      ...priorDrawn.map((p) => p.value),
      ...prior2Drawn.map((p) => p.value),
    )
    const niceMax = Math.max(1, maxVal * 1.1)
    const plotW = width - padL - padR
    const plotH = height - padT - padB
    const xAtFn = (t: number) => padL + ((t - t0) / span) * plotW
    const yAtFn = (v: number) => padT + plotH - (Math.min(v, niceMax) / niceMax) * plotH
    const tAtFn = (x: number) => t0 + ((x - padL) / plotW) * span
    const xy = drawPoints.map((p) => ({ x: xAtFn(p.t), y: yAtFn(p.value) }))
    const priorXY = priorDrawn.map((p) => ({ x: xAtFn(p.t), y: yAtFn(p.value) }))
    const prior2XY = prior2Drawn.map((p) => ({ x: xAtFn(p.t), y: yAtFn(p.value) }))
    const d = seriesPath(xy)
    const priorD = priorXY.length ? seriesPath(priorXY) : ''
    const prior2D = prior2XY.length ? seriesPath(prior2XY) : ''
    const xClose0 = xy[0].x.toFixed(1)
    const xClose1 = xy[xy.length - 1].x.toFixed(1)
    const areaD = `${d} L${xClose1},${(padT + plotH).toFixed(1)} L${xClose0},${(padT + plotH).toFixed(1)} Z`
    const labels: { x: number; label: string }[] = []
    const n = hours <= 1 ? 3 : hours <= 24 ? 4 : 5
    for (let i = 0; i < n; i++) {
      const t = t0 + (span * i) / Math.max(n - 1, 1)
      labels.push({ x: xAtFn(t), label: formatTick(t, hours) })
    }
    return {
      path: d,
      priorPath: priorD,
      prior2Path: prior2D,
      area: areaD,
      maxY: niceMax,
      xLabels: labels,
      xAt: xAtFn,
      yAt: yAtFn,
      tAt: tAtFn,
    }
  }, [drawPoints, priorDrawn, prior2Drawn, points, hours, padL])

  function nearest(t: number): MetricPoint | null {
    if (!points.length) return null
    let lo = 0
    let hi = points.length - 1
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (points[mid].t < t) lo = mid + 1
      else hi = mid
    }
    const cur = points[lo]
    const prev = lo > 0 ? points[lo - 1] : cur
    return Math.abs(prev.t - t) <= Math.abs(cur.t - t) ? prev : cur
  }

  function infoAtX(svgX: number): PointInfo | null {
    const p = nearest(tAt(svgX))
    if (!p) return null
    const slack = weekSlackMs(hours)
    const prev = nearestByTime(prior, p.t - WEEK_MS, slack)
    const prev2 = nearestByTime(prior2, p.t - DAY_MS, slack)
    return {
      x: xAt(p.t),
      y: yAt(p.value),
      ...formatHover(p.t, p.value, formatValue),
      priorText: prev ? formatValue(prev.value) : undefined,
      priorWhen: prev ? formatWeekWhen(prev.t) : undefined,
      deltaText: prev ? signedDelta(p.value, prev.value, formatValue) : undefined,
      prior2Text: prev2 ? formatValue(prev2.value) : undefined,
      prior2When: prev2 ? formatWeekWhen(prev2.t) : undefined,
      delta2Text: prev2 ? signedDelta(p.value, prev2.value, formatValue) : undefined,
    }
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
    if (e.pointerType === 'mouse' && info) {
      setActive((prev) => (prev && prev.x === info.x && prev.value === info.value ? prev : info))
    }
  }

  function onContext(e: ReactMouseEvent<SVGSVGElement>) {
    if (!onContextPoint) return
    e.preventDefault()
    const rect = e.currentTarget.getBoundingClientRect()
    const x = ((e.clientX - rect.left) / rect.width) * width
    const info = infoAtX(x)
    if (info) setActive(info)
    const p = nearest(tAt(x))
    if (!p) return
    onContextPoint({ t: p.t, clientX: e.clientX, clientY: e.clientY })
  }

  function onDown(e: ReactPointerEvent<SVGSVGElement>) {
    if (e.button !== 0) return
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

  const legendValue =
    current != null && Number.isFinite(current)
      ? current
      : points.length
        ? points[points.length - 1].value
        : null

  const windowLabel =
    hours <= 1
      ? 'последний час'
      : hours < 48
        ? `последние ${hours} ч`
        : hours <= 168
          ? 'последние 7 дней'
          : 'последние 30 дней'

  return (
    <div className={`online-chart${className ? ` ${className}` : ''}`}>
      {showLegend ? (
        <div className="traffic-chart-legend">
          <span className="traffic-legend-item tx">
            <span className="traffic-legend-swatch" aria-hidden />
            Онлайн пользователей
            {legendValue != null ? (
              <span className="traffic-legend-val">{formatValue(legendValue)}</span>
            ) : null}
          </span>
          {prior.length ? (
            <span className="traffic-legend-item prior">
              <span className="traffic-legend-swatch" aria-hidden />
              Прошлая неделя
            </span>
          ) : null}
          {prior2.length ? (
            <span className="traffic-legend-item prior prior2">
              <span className="traffic-legend-swatch" aria-hidden />
              Предыдущий день
            </span>
          ) : null}
          <span className="traffic-chart-window muted">{windowLabel}</span>
        </div>
      ) : null}
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
          onContextMenu={onContext}
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
          {prior2Path ? <path d={prior2Path} className="online-chart-line prior prior2" /> : null}
          {priorPath ? <path d={priorPath} className="online-chart-line prior" /> : null}
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
              <line
                x1={padL}
                x2={width - padR}
                y1={active.y}
                y2={active.y}
                className="online-chart-cross-h"
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
            {active.priorText ? (
              <div className="online-chart-plaque-online prior">
                Неделю назад: <span className="mono">{active.priorText}</span>
                {active.priorWhen ? (
                  <span className="online-chart-plaque-prior-when"> {active.priorWhen}</span>
                ) : null}
                {active.deltaText ? (
                  <span className="online-chart-plaque-prior-when"> · {active.deltaText}</span>
                ) : null}
              </div>
            ) : null}
            {active.prior2Text ? (
              <div className="online-chart-plaque-online prior prior2">
                Вчера: <span className="mono">{active.prior2Text}</span>
                {active.prior2When ? (
                  <span className="online-chart-plaque-prior-when"> {active.prior2When}</span>
                ) : null}
                {active.delta2Text ? (
                  <span className="online-chart-plaque-prior-when"> · {active.delta2Text}</span>
                ) : null}
              </div>
            ) : null}
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
