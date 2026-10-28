/** Previous-week samples, shifted onto the same weekday and clock time. */

export const WEEK_MS = 7 * 24 * 60 * 60 * 1000

export function nearestByTime<T extends { t: number }>(points: T[], t: number, slackMs: number): T | null {
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
  const best = Math.abs(prev.t - t) <= Math.abs(cur.t - t) ? prev : cur
  if (Math.abs(best.t - t) > slackMs) return null
  return best
}

/** Raw 5-minute samples stay close. Hourly buckets can sit up to half an hour off. */
export function weekSlackMs(hours: number): number {
  return hours <= 1 ? 8 * 60 * 1000 : 45 * 60 * 1000
}

export function signedDelta(current: number, previous: number, format: (n: number) => string): string {
  const d = current - previous
  if (!Number.isFinite(d)) return ''
  const body = format(Math.abs(d))
  if (d > 0) return `+${body}`
  if (d < 0) return `−${body}`
  return body
}

export function formatWeekWhen(ts: number): string {
  return new Date(ts).toLocaleString('ru-RU', {
    timeZone: 'Europe/Moscow',
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}
