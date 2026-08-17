import { useMemo, useState } from 'react'
import { formatBytes } from '../api'

export type UsagePoint = {
  t: number
  rx_bytes: number
  tx_bytes: number
}

type Props = {
  points: UsagePoint[]
  totalRx: number
  totalTx: number
}

type Hover = {
  label: string
  rx: number
  tx: number
  x: number
}

const MSK = 'Europe/Moscow'

function mskDayKey(ts: number): string {
  return new Date(ts).toLocaleDateString('en-CA', { timeZone: MSK })
}

function formatDay(ymd: string): string {
  const [y, m, d] = ymd.split('-')
  if (!d) return ymd
  return `${d}.${m}` + (y ? `` : '')
}

function formatDayFull(ymd: string): string {
  const [y, m, d] = ymd.split('-')
  if (!d) return ymd
  return `${d}.${m}.${y}`
}

function pickScale(maxBytes: number): { div: number; suffix: string } {
  if (maxBytes >= 1024 ** 4) return { div: 1024 ** 4, suffix: 'TB' }
  if (maxBytes >= 1024 ** 3) return { div: 1024 ** 3, suffix: 'GB' }
  if (maxBytes >= 1024 ** 2) return { div: 1024 ** 2, suffix: 'MB' }
  if (maxBytes >= 1024) return { div: 1024, suffix: 'KB' }
  return { div: 1, suffix: 'B' }
}

export default function TrafficUsageChart({ points, totalRx, totalTx }: Props) {
  const days = useMemo(() => {
    const map = new Map<string, { rx: number; tx: number }>()
    for (const p of points) {
      const key = mskDayKey(p.t)
      const cur = map.get(key) || { rx: 0, tx: 0 }
      cur.rx += Number(p.rx_bytes) || 0
      cur.tx += Number(p.tx_bytes) || 0
      map.set(key, cur)
    }
    return [...map.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, v]) => ({ key, ...v }))
  }, [points])

  const max = Math.max(1, ...days.map((d) => Math.max(d.rx, d.tx)))
  const scale = pickScale(max)
  const [hover, setHover] = useState<Hover | null>(null)

  const width = 720
  const height = 220
  const padL = 52
  const padR = 12
  const padT = 12
  const padB = 28
  const innerW = width - padL - padR
  const innerH = height - padT - padB
  const n = Math.max(days.length, 1)
  const slot = innerW / n
  const barW = Math.max(2, Math.min(14, slot * 0.32))

  const ticks = 4
  const yTicks = Array.from({ length: ticks + 1 }, (_, i) => (max * i) / ticks)

  return (
    <div className="traffic-chart traffic-usage-chart">
      <div className="traffic-usage-totals">
        <div className="traffic-usage-total tx">
          <span className="muted">Исходящий</span>
          <strong>{formatBytes(totalTx)}</strong>
        </div>
        <div className="traffic-usage-total rx">
          <span className="muted">Входящий</span>
          <strong>{formatBytes(totalRx)}</strong>
        </div>
      </div>
      <svg
        className="traffic-chart-svg"
        viewBox={`0 0 ${width} ${height}`}
        onMouseLeave={() => setHover(null)}
      >
        {yTicks.map((v, i) => {
          const y = padT + innerH - (v / max) * innerH
          return (
            <g key={i}>
              <line x1={padL} x2={width - padR} y1={y} y2={y} className="traffic-grid" />
              <text x={padL - 6} y={y + 3} textAnchor="end" className="traffic-axis">
                {v === 0 ? '0' : `${(v / scale.div).toFixed(v / scale.div >= 10 ? 0 : 1)} ${scale.suffix}`}
              </text>
            </g>
          )
        })}
        {days.map((d, i) => {
          const cx = padL + slot * i + slot / 2
          const txH = (d.tx / max) * innerH
          const rxH = (d.rx / max) * innerH
          const txY = padT + innerH - txH
          const rxY = padT + innerH - rxH
          return (
            <g
              key={d.key}
              onMouseEnter={() =>
                setHover({
                  label: formatDayFull(d.key),
                  rx: d.rx,
                  tx: d.tx,
                  x: cx,
                })
              }
            >
              <rect
                x={cx - barW - 1}
                y={txY}
                width={barW}
                height={Math.max(txH, 0)}
                className="traffic-usage-bar tx"
              />
              <rect
                x={cx + 1}
                y={rxY}
                width={barW}
                height={Math.max(rxH, 0)}
                className="traffic-usage-bar rx"
              />
            </g>
          )
        })}
        {days.length === 0 ? (
          <text x={width / 2} y={height / 2} textAnchor="middle" className="traffic-empty">
            За эти даты пока нет данных
          </text>
        ) : null}
        {days.map((d, i) => {
          const show = days.length <= 10 || i % Math.ceil(days.length / 8) === 0 || i === days.length - 1
          if (!show) return null
          const cx = padL + slot * i + slot / 2
          return (
            <text key={d.key} x={cx} y={height - 8} textAnchor="middle" className="traffic-axis">
              {formatDay(d.key)}
            </text>
          )
        })}
      </svg>
      {hover ? (
        <div className="traffic-usage-tip">
          <span className="mono">{hover.label}</span>
          <span className="traffic-legend-item tx">
            <span className="traffic-legend-swatch" />
            исх. {formatBytes(hover.tx)}
          </span>
          <span className="traffic-legend-item rx">
            <span className="traffic-legend-swatch" />
            вх. {formatBytes(hover.rx)}
          </span>
        </div>
      ) : (
        <div className="traffic-chart-legend">
          <span className="traffic-legend-item tx">
            <span className="traffic-legend-swatch" />
            Исходящий (TX)
          </span>
          <span className="traffic-legend-item rx">
            <span className="traffic-legend-swatch" />
            Входящий (RX)
          </span>
          <span className="traffic-chart-window muted">сутки, Europe/Moscow</span>
        </div>
      )}
    </div>
  )
}
