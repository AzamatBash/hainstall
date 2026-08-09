import { useMemo } from 'react'
import type { AnalyticsBucket } from '../api'

export type SegmentSeries = {
  key: string
  label: string
  color: string
  onlineNow: number
  hint?: string
  points: { t: number; online: number }[]
}

type Props = {
  series: SegmentSeries[]
}

function formatTick(ts: number) {
  const d = new Date(ts)
  return d.toLocaleString('ru-RU', {
    timeZone: 'Europe/Moscow',
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** Build hourly series for one segment key from analytics buckets. */
export function seriesFromBuckets(buckets: AnalyticsBucket[], key: string) {
  return buckets
    .filter((b) => b.key === key)
    .map((b) => ({ t: b.t, online: b.online }))
    .sort((a, b) => a.t - b.t)
}

export default function SegmentWeekChart({ series }: Props) {
  const width = 720
  const height = 280
  const padL = 44
  const padR = 14
  const padT = 16
  const padB = 36

  const { paths, maxY, xLabels } = useMemo(() => {
    const all = series.flatMap((s) => s.points)
    if (!all.length) {
      return { paths: [] as { key: string; color: string; d: string }[], maxY: 1, xLabels: [] as { x: number; label: string }[] }
    }
    const t0 = Math.min(...all.map((p) => p.t))
    const t1 = Math.max(...all.map((p) => p.t))
    const span = Math.max(t1 - t0, 1)
    const maxOnline = Math.max(1, ...all.map((p) => p.online))
    const niceMax = Math.max(1, Math.ceil(maxOnline * 1.12))
    const plotW = width - padL - padR
    const plotH = height - padT - padB
    const xAt = (t: number) => padL + ((t - t0) / span) * plotW
    const yAt = (v: number) => padT + plotH - (Math.min(v, niceMax) / niceMax) * plotH
    const pathsOut = series.map((s) => {
      if (!s.points.length) return { key: s.key, color: s.color, d: '' }
      const d = s.points
        .map((p, i) => `${i === 0 ? 'M' : 'L'}${xAt(p.t).toFixed(1)},${yAt(p.online).toFixed(1)}`)
        .join(' ')
      return { key: s.key, color: s.color, d }
    })
    const labels: { x: number; label: string }[] = []
    for (let i = 0; i < 5; i++) {
      const t = t0 + (span * i) / 4
      labels.push({ x: xAt(t), label: formatTick(t) })
    }
    return { paths: pathsOut, maxY: niceMax, xLabels: labels }
  }, [series])

  const hasHistory = series.some((s) => s.points.length > 1)

  return (
    <div className="segment-week-chart">
      <svg
        className="segment-week-chart-svg"
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label="Онлайн по сегментам за неделю"
      >
        {[0, 0.25, 0.5, 0.75, 1].map((f) => {
          const y = padT + (height - padT - padB) * (1 - f)
          const val = Math.round(maxY * f)
          return (
            <g key={f}>
              <line
                x1={padL}
                x2={width - padR}
                y1={y}
                y2={y}
                className="segment-week-grid"
              />
              <text x={padL - 8} y={y + 4} textAnchor="end" className="segment-week-axis">
                {val}
              </text>
            </g>
          )
        })}
        {paths.map((p) =>
          p.d ? (
            <path key={p.key} d={p.d} fill="none" stroke={p.color} strokeWidth={2.2} />
          ) : null,
        )}
        {xLabels.map((l) => (
          <text
            key={l.x}
            x={l.x}
            y={height - 10}
            textAnchor="middle"
            className="segment-week-axis"
          >
            {l.label}
          </text>
        ))}
      </svg>
      {!hasHistory ? (
        <p className="muted segment-week-empty">
          История появится после нескольких опросов (раз в 5 минут). Сейчас — только актуальные цифры сверху.
        </p>
      ) : null}
    </div>
  )
}
