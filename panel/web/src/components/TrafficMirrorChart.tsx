import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import { formatBitrateShort } from '../api'

export type TrafficPoint = {
  t: number
  down_bps: number
  up_bps: number
}

type Props = {
  points: TrafficPoint[]
  /** Selected window length in hours (legend / axis). */
  hours?: number
  className?: string
  /** `stats` matches OnlineUsersChart block size + hover/zoom on /stats. */
  size?: 'compact' | 'stats'
  onZoom?: (from: number, to: number) => void
  currentDownBps?: number | null
  currentUpBps?: number | null
}

type PointInfo = {
  x: number
  yTx: number
  yRx: number
  date: string
  time: string
  downText: string
  upText: string
}

const TAP_SLOP = 14
const MAX_DRAW_POINTS = 1200

function downsample(points: TrafficPoint[], max: number): TrafficPoint[] {
  if (points.length <= max) return points
  const out: TrafficPoint[] = []
  const step = (points.length - 1) / (max - 1)
  for (let i = 0; i < max; i++) {
    out.push(points[Math.round(i * step)])
  }
  return out
}

function toMbit(bytesPerSec: number): number {
  if (!Number.isFinite(bytesPerSec) || bytesPerSec <= 0) return 0
  return (bytesPerSec * 8) / 1_000_000
}

function niceMax(v: number): number {
  if (v <= 1) return 1
  if (v <= 5) return 5
  if (v <= 10) return 10
  if (v <= 25) return 25
  if (v <= 50) return 50
  if (v <= 100) return 100
  if (v <= 250) return 250
  if (v <= 500) return 500
  if (v <= 1000) return 1000
  return Math.ceil(v / 500) * 500
}

function formatAxisTime(ts: number, hours: number) {
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

function formatHover(ts: number, downBps: number, upBps: number): Omit<PointInfo, 'x' | 'yTx' | 'yRx'> {
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
  return {
    date,
    time: `${time} МСК`,
    downText: formatBitrateShort(downBps),
    upText: formatBitrateShort(upBps),
  }
}

function Plaque({ info, className }: { info: PointInfo; className?: string }) {
  return (
    <div className={className} role="tooltip">
      <div className="online-chart-plaque-date">{info.date}</div>
      <div className="online-chart-plaque-time">{info.time}</div>
      <div className="online-chart-plaque-online">
        Отдача TX: <span className="mono">{info.downText}</span>
      </div>
      <div className="online-chart-plaque-online">
        Загрузка RX: <span className="mono">{info.upText}</span>
      </div>
    </div>
  )
}

/** Mirrored TX/RX area chart (panel style). */
export default function TrafficMirrorChart({
  points,
  hours = 1,
  className,
  size = 'compact',
  onZoom,
  currentDownBps = null,
  currentUpBps = null,
}: Props) {
  const stats = size === 'stats'
  const width = 720
  const height = stats ? 260 : 200
  const padL = stats ? 44 : 48
  const padR = 12
  const padT = stats ? 18 : 14
  const padB = stats ? 30 : 28
  const maxPoints = hours >= 168 ? 800 : MAX_DRAW_POINTS

  const [active, setActive] = useState<PointInfo | null>(null)
  const dragRef = useRef<{
    startX: number
    startT: number
    pointerId: number
    moved: boolean
  } | null>(null)
  const [dragX, setDragX] = useState<number | null>(null)

  useEffect(() => {
    setActive(null)
  }, [points, hours])

  const {
    pathUp,
    pathDown,
    areaUp,
    areaDown,
    maxMbit,
    yTicks,
    xLabels,
    lastUp,
    lastDown,
    xAt,
    yTx,
    yRx,
    tAt,
  } = useMemo(() => {
    const empty = {
      pathUp: '',
      pathDown: '',
      areaUp: '',
      areaDown: '',
      maxMbit: 1,
      yTicks: [1, 0.5, 0, -0.5, -1] as number[],
      xLabels: [] as Array<{ x: number; label: string }>,
      lastUp: 0,
      lastDown: 0,
      xAt: (_t: number) => padL,
      yTx: (_v: number) => padT,
      yRx: (_v: number) => padT,
      tAt: (_x: number) => 0,
    }
    const drawn = downsample(points, maxPoints)
    if (!drawn.length) return empty

    // down_bps = host TX (outbound), up_bps = host RX (inbound)
    const txVals = drawn.map((p) => toMbit(p.down_bps))
    const rxVals = drawn.map((p) => toMbit(p.up_bps))
    const peak = Math.max(0.01, ...txVals, ...rxVals)
    const maxM = niceMax(peak)
    const minT = drawn[0].t
    const maxT = drawn[drawn.length - 1].t
    const spanT = Math.max(maxT - minT, 1)
    const innerW = width - padL - padR
    const innerH = height - padT - padB
    const midY = padT + innerH / 2

    const xAtFn = (t: number) => padL + ((t - minT) / spanT) * innerW
    const yTxFn = (mbit: number) => midY - (Math.min(mbit, maxM) / maxM) * (innerH / 2)
    const yRxFn = (mbit: number) => midY + (Math.min(mbit, maxM) / maxM) * (innerH / 2)
    const tAtFn = (x: number) => minT + ((x - padL) / innerW) * spanT

    const txPath = drawn
      .map((p, i) => {
        const x = xAtFn(p.t)
        const y = yTxFn(toMbit(p.down_bps))
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
    const rxPath = drawn
      .map((p, i) => {
        const x = xAtFn(p.t)
        const y = yRxFn(toMbit(p.up_bps))
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')

    const x0 = xAtFn(drawn[0].t)
    const x1 = xAtFn(drawn[drawn.length - 1].t)
    const areaTx = `${txPath} L${x1.toFixed(1)},${midY.toFixed(1)} L${x0.toFixed(1)},${midY.toFixed(1)} Z`
    const areaRx = `${rxPath} L${x1.toFixed(1)},${midY.toFixed(1)} L${x0.toFixed(1)},${midY.toFixed(1)} Z`

    const ticks = [maxM, maxM / 2, 0, -maxM / 2, -maxM]
    const labelCount = hours <= 1 ? 3 : hours <= 24 ? 4 : 5
    const labels: Array<{ x: number; label: string }> = []
    for (let i = 0; i < labelCount; i++) {
      const t = minT + (spanT * i) / Math.max(labelCount - 1, 1)
      labels.push({ x: xAtFn(t), label: formatAxisTime(t, hours) })
    }

    return {
      pathUp: txPath,
      pathDown: rxPath,
      areaUp: areaTx,
      areaDown: areaRx,
      maxMbit: maxM,
      yTicks: ticks,
      xLabels: labels,
      lastUp: txVals[txVals.length - 1] ?? 0,
      lastDown: rxVals[rxVals.length - 1] ?? 0,
      xAt: xAtFn,
      yTx: yTxFn,
      yRx: yRxFn,
      tAt: tAtFn,
    }
  }, [points, hours, maxPoints, width, height, padL, padR, padT, padB])

  const innerH = height - padT - padB
  const midY = padT + innerH / 2
  const last = points.length ? points[points.length - 1] : null
  const legendDown =
    currentDownBps != null && Number.isFinite(currentDownBps)
      ? currentDownBps
      : last?.down_bps
  const legendUp =
    currentUpBps != null && Number.isFinite(currentUpBps) ? currentUpBps : last?.up_bps
  const windowLabel =
    hours <= 1
      ? 'последний час · Mbit/s'
      : hours < 48
        ? `последние ${hours} ч · Mbit/s`
        : hours <= 168
          ? 'последние 7 дней · Mbit/s'
          : 'последние 30 дней · Mbit/s'

  function nearest(t: number): TrafficPoint | null {
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
    return {
      x: xAt(p.t),
      yTx: yTx(toMbit(p.down_bps)),
      yRx: yRx(toMbit(p.up_bps)),
      ...formatHover(p.t, p.down_bps, p.up_bps),
    }
  }

  function svgXFromEvent(e: ReactPointerEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    return ((e.clientX - rect.left) / rect.width) * width
  }

  function onMove(e: ReactPointerEvent<SVGSVGElement>) {
    if (!stats) return
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
    if (!stats) return
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
    if (!stats) return
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

  const dragBand =
    stats && dragRef.current?.moved && dragX != null
      ? {
          x: Math.min(dragRef.current.startX, dragX),
          w: Math.abs(dragX - dragRef.current.startX),
        }
      : null

  const tipLeftPct = active ? (active.x / width) * 100 : 0
  const tipTopPct = active ? (active.yTx / height) * 100 : 0
  const tipFlipX = tipLeftPct > 62
  const tipFlipY = tipTopPct < 28

  const rootClass = stats
    ? `online-chart traffic-chart-stats${className ? ` ${className}` : ''}`
    : `traffic-chart${className ? ` ${className}` : ''}`

  const svg = (
    <svg
      className={stats ? 'online-chart-svg' : 'traffic-chart-svg'}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      role="img"
      aria-label={`Трафик: отдача ${lastUp.toFixed(1)} Mbit/s, загрузка ${lastDown.toFixed(1)} Mbit/s`}
      onPointerMove={stats ? onMove : undefined}
      onPointerLeave={
        stats
          ? (e) => {
              if (e.pointerType === 'mouse') {
                if (!dragRef.current) setActive(null)
              }
              dragRef.current = null
              setDragX(null)
            }
          : undefined
      }
      onPointerDown={stats ? onDown : undefined}
      onPointerUp={stats ? onUp : undefined}
      onPointerCancel={
        stats
          ? () => {
              dragRef.current = null
              setDragX(null)
            }
          : undefined
      }
    >
      {yTicks.map((tick) => {
        const abs = Math.abs(tick)
        const y =
          tick >= 0
            ? midY - (abs / maxMbit) * (innerH / 2)
            : midY + (abs / maxMbit) * (innerH / 2)
        return (
          <g key={`yt-${tick}`}>
            <line
              x1={padL}
              x2={width - padR}
              y1={y}
              y2={y}
              className={tick === 0 ? 'traffic-grid zero' : 'traffic-grid'}
            />
            <text x={padL - 6} y={y + 3} textAnchor="end" className="traffic-axis">
              {tick === 0 ? '0' : String(Math.round(abs))}
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
      {areaUp ? <path d={areaUp} className="traffic-area tx" /> : null}
      {areaDown ? <path d={areaDown} className="traffic-area rx" /> : null}
      {pathUp ? <path d={pathUp} className="traffic-line tx" /> : null}
      {pathDown ? <path d={pathDown} className="traffic-line rx" /> : null}
      {!points.length ? (
        <text x={width / 2} y={height / 2} textAnchor="middle" className="traffic-empty">
          Пока нет точек — подождите первый опрос (до 5 мин)
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
          <circle cx={active.x} cy={active.yTx} r={4.5} className="online-chart-dot traffic-dot-tx" />
          <circle cx={active.x} cy={active.yRx} r={4.5} className="online-chart-dot traffic-dot-rx" />
        </>
      ) : null}
      {xLabels.map((l) => (
        <text key={l.label + l.x} x={l.x} y={height - 8} textAnchor="middle" className="traffic-axis">
          {l.label}
        </text>
      ))}
    </svg>
  )

  return (
    <div className={rootClass}>
      <div className="traffic-chart-legend">
        <span className="traffic-legend-item tx">
          <span className="traffic-legend-swatch" aria-hidden />
          Отдача TX
          {legendDown != null ? (
            <span className="traffic-legend-val">{formatBitrateShort(legendDown)}</span>
          ) : null}
        </span>
        <span className="traffic-legend-item rx">
          <span className="traffic-legend-swatch" aria-hidden />
          Загрузка RX
          {legendUp != null ? (
            <span className="traffic-legend-val">{formatBitrateShort(legendUp)}</span>
          ) : null}
        </span>
        <span className="traffic-chart-window muted">{windowLabel}</span>
      </div>
      {stats ? (
        <>
          <div className="online-chart-frame">
            {svg}
            {active && !dragRef.current?.moved ? (
              <div
                className={`online-chart-plaque online-chart-plaque-float${tipFlipX ? ' flip-x' : ''}${tipFlipY ? ' flip-y' : ''}`}
                style={{ left: `${tipLeftPct}%`, top: `${tipTopPct}%` }}
                role="tooltip"
              >
                <div className="online-chart-plaque-date">{active.date}</div>
                <div className="online-chart-plaque-time">{active.time}</div>
                <div className="online-chart-plaque-online">
                  Отдача TX: <span className="mono">{active.downText}</span>
                </div>
                <div className="online-chart-plaque-online">
                  Загрузка RX: <span className="mono">{active.upText}</span>
                </div>
              </div>
            ) : null}
          </div>
          {active ? (
            <Plaque info={active} className="online-chart-plaque online-chart-plaque-dock" />
          ) : (
            <p className="online-chart-tap-hint muted">
              Нажмите на график, чтобы увидеть дату и трафик
            </p>
          )}
        </>
      ) : (
        svg
      )}
    </div>
  )
}
