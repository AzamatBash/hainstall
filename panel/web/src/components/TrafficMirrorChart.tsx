import { useMemo } from 'react'
import { formatBitrateShort } from '../api'

export type TrafficPoint = {
  t: number
  down_bps: number
  up_bps: number
}

type Props = {
  points: TrafficPoint[]
  /** Compact height for nodes list rows. */
  compact?: boolean
  className?: string
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

function formatAxisTime(ts: number): string {
  const d = new Date(ts)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  return `${hh}:${mm}`
}

/** Mirrored TX/RX area chart for the last hour (panel style). */
export default function TrafficMirrorChart({ points, compact, className }: Props) {
  const width = compact ? 420 : 720
  const height = compact ? 96 : 220
  const padL = compact ? 36 : 48
  const padR = 12
  const padT = compact ? 8 : 14
  const padB = compact ? 18 : 28

  const { pathUp, pathDown, areaUp, areaDown, maxMbit, yTicks, xLabels, lastUp, lastDown } =
    useMemo(() => {
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
      }
      if (!points.length) return empty

      const upVals = points.map((p) => toMbit(p.down_bps))
      const downVals = points.map((p) => toMbit(p.up_bps))
      const peak = Math.max(0.01, ...upVals, ...downVals)
      const maxM = niceMax(peak)
      const minT = points[0].t
      const maxT = points[points.length - 1].t
      const spanT = Math.max(maxT - minT, 1)
      const innerW = width - padL - padR
      const innerH = height - padT - padB
      const midY = padT + innerH / 2

      const xAt = (t: number) => padL + ((t - minT) / spanT) * innerW
      const yUp = (mbit: number) => midY - (Math.min(mbit, maxM) / maxM) * (innerH / 2)
      const yDown = (mbit: number) => midY + (Math.min(mbit, maxM) / maxM) * (innerH / 2)

      const upPath = points
        .map((p, i) => {
          const x = xAt(p.t)
          const y = yUp(toMbit(p.down_bps))
          return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
        })
        .join(' ')
      const downPath = points
        .map((p, i) => {
          const x = xAt(p.t)
          const y = yDown(toMbit(p.up_bps))
          return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
        })
        .join(' ')

      const x0 = xAt(points[0].t)
      const x1 = xAt(points[points.length - 1].t)
      const areaU = `${upPath} L${x1.toFixed(1)},${midY.toFixed(1)} L${x0.toFixed(1)},${midY.toFixed(1)} Z`
      const areaD = `${downPath} L${x1.toFixed(1)},${midY.toFixed(1)} L${x0.toFixed(1)},${midY.toFixed(1)} Z`

      const ticks = [maxM, maxM / 2, 0, -maxM / 2, -maxM]
      const labelCount = compact ? 4 : 6
      const labels: Array<{ x: number; label: string }> = []
      for (let i = 0; i < labelCount; i++) {
        const t = minT + (spanT * i) / Math.max(labelCount - 1, 1)
        labels.push({ x: xAt(t), label: formatAxisTime(t) })
      }

      return {
        pathUp: upPath,
        pathDown: downPath,
        areaUp: areaU,
        areaDown: areaD,
        maxMbit: maxM,
        yTicks: ticks,
        xLabels: labels,
        lastUp: upVals[upVals.length - 1] ?? 0,
        lastDown: downVals[downVals.length - 1] ?? 0,
      }
    }, [points, width, height, padL, padR, padT, padB, compact])

  const innerH = height - padT - padB
  const midY = padT + innerH / 2
  const last = points.length ? points[points.length - 1] : null

  return (
    <div className={`traffic-chart${compact ? ' compact' : ''}${className ? ` ${className}` : ''}`}>
      <div className="traffic-chart-legend">
        <span className="traffic-legend-item tx">
          <span className="traffic-legend-swatch" aria-hidden />
          Отдача TX
          {last ? (
            <span className="traffic-legend-val">{formatBitrateShort(last.down_bps)}</span>
          ) : null}
        </span>
        <span className="traffic-legend-item rx">
          <span className="traffic-legend-swatch" aria-hidden />
          Загрузка RX
          {last ? (
            <span className="traffic-legend-val">{formatBitrateShort(last.up_bps)}</span>
          ) : null}
        </span>
        {!compact ? <span className="traffic-chart-window muted">последний час · Mbit/s</span> : null}
      </div>
      <svg
        className="traffic-chart-svg"
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={`Трафик: отдача ${lastUp.toFixed(1)} Mbit/s, загрузка ${lastDown.toFixed(1)} Mbit/s`}
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
        {areaUp ? <path d={areaUp} className="traffic-area tx" /> : null}
        {areaDown ? <path d={areaDown} className="traffic-area rx" /> : null}
        {pathUp ? <path d={pathUp} className="traffic-line tx" /> : null}
        {pathDown ? <path d={pathDown} className="traffic-line rx" /> : null}
        {!points.length ? (
          <text x={width / 2} y={height / 2} textAnchor="middle" className="traffic-empty">
            Накапливаем историю…
          </text>
        ) : null}
        {xLabels.map((l) => (
          <text key={l.label + l.x} x={l.x} y={height - 6} textAnchor="middle" className="traffic-axis">
            {l.label}
          </text>
        ))}
      </svg>
    </div>
  )
}
