import { useMemo } from 'react'

export type ChartPoint = { t: number; v: number }

type Props = {
  label: string
  unit?: string
  valueLabel: string
  points: ChartPoint[]
  color: string
  max?: number
  height?: number
}

/** Compact Remnawave-like SVG polyline chart (no chart library). */
export default function SparklineChart({
  label,
  unit = '%',
  valueLabel,
  points,
  color,
  max = 100,
  height = 72,
}: Props) {
  const width = 320
  const padX = 4
  const padY = 6

  const path = useMemo(() => {
    if (points.length < 2) return ''
    const minT = points[0].t
    const maxT = points[points.length - 1].t || minT + 1
    const spanT = Math.max(maxT - minT, 1)
    const innerW = width - padX * 2
    const innerH = height - padY * 2
    return points
      .map((p, i) => {
        const x = padX + ((p.t - minT) / spanT) * innerW
        const y = padY + innerH - (Math.min(Math.max(p.v, 0), max) / max) * innerH
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  }, [points, max, height])

  const area = useMemo(() => {
    if (!path || points.length < 2) return ''
    const xLast = width - padX
    const yBase = height - padY
    return `${path} L${xLast.toFixed(1)},${yBase} L${padX},${yBase} Z`
  }, [path, height])

  return (
    <div className="metric-chart">
      <div className="metric-chart-head">
        <span className="metric-chart-label">{label}</span>
        <span className="metric-chart-value" style={{ color }}>
          {valueLabel}
          {unit ? <span className="metric-chart-unit">{unit}</span> : null}
        </span>
      </div>
      <svg
        className="metric-chart-svg"
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={`${label}: ${valueLabel}${unit}`}
      >
        <line
          x1={padX}
          x2={width - padX}
          y1={height / 2}
          y2={height / 2}
          className="metric-chart-grid"
        />
        {area && <path d={area} fill={color} opacity={0.12} />}
        {path ? (
          <path d={path} fill="none" stroke={color} strokeWidth={1.75} strokeLinejoin="round" />
        ) : (
          <text x={width / 2} y={height / 2 + 4} textAnchor="middle" className="metric-chart-empty">
            …
          </text>
        )}
      </svg>
    </div>
  )
}
