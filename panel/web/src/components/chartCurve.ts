/** Time-bucket averages and a monotone curve for the stats charts. */

const HOUR = 3_600_000

export type XY = { x: number; y: number }

/** Bucket width for a visible span. Null means draw the raw 5-minute samples. An hour stays raw; 24h and longer are hourly. */
export function bucketMsForSpan(spanMs: number): number | null {
  if (!Number.isFinite(spanMs) || spanMs <= HOUR) return null
  return HOUR
}

/**
 * Average samples into equal time buckets. Empty buckets are omitted.
 * The point timestamp is the middle of the bucket.
 */
export function bucketBySpan<T extends { t: number }>(
  points: T[],
  mean: (group: T[], center: number) => T,
): T[] {
  if (points.length < 2) return points
  const span = points[points.length - 1].t - points[0].t
  const width = bucketMsForSpan(span)
  if (width == null) return points
  const groups = new Map<number, T[]>()
  for (const p of points) {
    const start = Math.floor(p.t / width) * width
    const group = groups.get(start)
    if (group) group.push(p)
    else groups.set(start, [p])
  }
  return [...groups.keys()]
    .sort((a, b) => a - b)
    .map((start) => mean(groups.get(start)!, start + width / 2))
}

function fmt(n: number): string {
  return n.toFixed(1)
}

function monotoneSlopes(xs: number[], ys: number[]): number[] {
  const n = xs.length
  const m = new Array<number>(n).fill(0)
  if (n < 2) return m
  const delta: number[] = []
  for (let i = 0; i < n - 1; i++) {
    const dx = xs[i + 1] - xs[i]
    delta.push(dx === 0 ? 0 : (ys[i + 1] - ys[i]) / dx)
  }
  m[0] = delta[0]
  m[n - 1] = delta[n - 2]
  for (let i = 1; i < n - 1; i++) {
    if (delta[i - 1] === 0 || delta[i] === 0 || delta[i - 1] * delta[i] < 0) m[i] = 0
    else m[i] = (delta[i - 1] + delta[i]) / 2
  }
  for (let i = 0; i < n - 1; i++) {
    if (delta[i] === 0) {
      m[i] = 0
      m[i + 1] = 0
      continue
    }
    const a = m[i] / delta[i]
    const b = m[i + 1] / delta[i]
    const h = a * a + b * b
    if (h > 9) {
      const t = 3 / Math.sqrt(h)
      m[i] = t * a * delta[i]
      m[i + 1] = t * b * delta[i]
    }
  }
  return m
}

function polyline(pts: XY[]): string {
  return pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${fmt(p.x)},${fmt(p.y)}`).join(' ')
}

/** Hourly windows with hundreds of points are drawn as a polyline. A curve through them costs the same picture and a much heavier SVG. */
export function seriesPath(pts: XY[]): string {
  if (pts.length > 48) return polyline(pts)
  return smoothLine(pts)
}

/** Monotone cubic through the points. Stays between them, so it does not overshoot a peak. */
export function smoothLine(pts: XY[]): string {
  if (pts.length < 3) return polyline(pts)
  const xs = pts.map((p) => p.x)
  const ys = pts.map((p) => p.y)
  const m = monotoneSlopes(xs, ys)
  let d = `M${fmt(xs[0])},${fmt(ys[0])}`
  for (let i = 0; i < pts.length - 1; i++) {
    const dx = xs[i + 1] - xs[i]
    if (dx === 0) {
      d += ` L${fmt(xs[i + 1])},${fmt(ys[i + 1])}`
      continue
    }
    const c1x = xs[i] + dx / 3
    const c1y = ys[i] + (m[i] * dx) / 3
    const c2x = xs[i + 1] - dx / 3
    const c2y = ys[i + 1] - (m[i + 1] * dx) / 3
    d += ` C${fmt(c1x)},${fmt(c1y)} ${fmt(c2x)},${fmt(c2y)} ${fmt(xs[i + 1])},${fmt(ys[i + 1])}`
  }
  return d
}
