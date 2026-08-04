export type NodeStatus = 'unknown' | 'online' | 'offline'

export interface Node {
  id: string
  name: string
  url: string
  created_at: string
  last_seen?: string
  status: NodeStatus
}

export interface BackendServer {
  backend: string
  name: string
  address: string
  port: number
  weight?: number
  status?: string
  [key: string]: unknown
}

export interface StatsSummary {
  active_connections?: number
  active_sessions?: number
  bytes_in?: number
  bytes_out?: number
  frontends?: Array<{
    name: string
    status: string
    sessions: number
    req_rate: number
    bytes_in?: number
    bytes_out?: number
  }>
  backends?: Array<{
    name: string
    status: string
    sessions: number
    servers_up: number
    bytes_in?: number
    bytes_out?: number
  }>
  [key: string]: unknown
}

export interface SystemMetrics {
  cpu_percent: number
  mem_used_bytes: number
  mem_total_bytes: number
  mem_percent: number
  load_avg?: number[]
  uptime_seconds?: number
  disk_used_bytes?: number
  disk_total_bytes?: number
  disk_percent?: number
  timestamp: string
}

export function formatBytes(n?: number | null): string {
  if (n == null || !Number.isFinite(n)) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** Format bits/s. Input is bytes per second. */
export function formatBitrate(bytesPerSec?: number | null): string {
  if (bytesPerSec == null || !Number.isFinite(bytesPerSec) || bytesPerSec < 0) return '—'
  const bits = bytesPerSec * 8
  const units = ['bit/s', 'Kbit/s', 'Mbit/s', 'Gbit/s']
  let v = bits
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function formatUptime(sec?: number | null): string {
  if (sec == null || !Number.isFinite(sec) || sec < 0) return '—'
  const s = Math.floor(sec)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}д ${h}ч`
  if (h > 0) return `${h}ч ${m}м`
  return `${m}м`
}

export function statusLabel(status?: string | null): string {
  switch (status) {
    case 'online':
      return 'Онлайн'
    case 'offline':
      return 'Офлайн'
    case 'unknown':
      return 'Не подключена'
    default:
      return status ? String(status) : 'Неизвестно'
  }
}

export function formatLastSeen(iso?: string | null): string {
  if (!iso) return 'Ещё не проверялась'
  try {
    return new Date(iso).toLocaleString('ru-RU')
  } catch {
    return iso
  }
}

const ERROR_MAP: Record<string, string> = {
  unauthorized: 'Нет доступа',
  'invalid password': 'Неверный пароль',
  'invalid json': 'Некорректный запрос',
  'token issue failed': 'Не удалось выдать токен',
  'db error': 'Ошибка базы данных',
  'node not found': 'Нода не найдена',
  'name and host are required': 'Укажите имя и хост',
  'укажите host или url': 'Укажите IP/домен или полный URL',
  'name, url, and token are required': 'Укажите имя, URL и токен',
  'url must start with https:// or http://': 'URL должен начинаться с https:// или http://',
  'unauthorized — token mismatch on node': 'Ошибка авторизации — неверный токен на ноде',
  'request failed': 'Запрос не выполнен',
  'read body failed': 'Не удалось прочитать ответ',
  'too many requests': 'Слишком много попыток. Подождите 15 мин.',
  'Too Many Requests': 'Слишком много попыток. Подождите 15 мин.',
}

export function translateError(msg: string): string {
  if (!msg) return 'Неизвестная ошибка'
  if (ERROR_MAP[msg]) return ERROR_MAP[msg]
  if (msg.startsWith('agent unreachable:')) {
    return 'Агент недоступен: ' + msg.slice('agent unreachable:'.length).trim()
  }
  if (msg.startsWith('unexpected status ')) {
    return 'Неожиданный ответ агента: ' + msg.slice('unexpected status '.length)
  }
  return msg
}

/** Internal HAProxy backends used for panel/agent/ACME — not user apps. */
const INTERNAL_BACKENDS = new Set(['hap_agent', 'acme'])

export function isInternalBackend(name?: string | null): boolean {
  if (!name) return false
  const n = name.trim().toLowerCase()
  if (INTERNAL_BACKENDS.has(n)) return true
  if (n.startsWith('hap_')) return true
  if (n.endsWith('_mgmt') || n.endsWith('_management')) return true
  if (n === 'stats' || n === 'prometheus') return true
  return false
}

export function filterUserBackends(servers: BackendServer[]): BackendServer[] {
  return servers.filter((s) => !isInternalBackend(String(s.backend ?? '')))
}

/** Sessions / UP counts from application backends only (exclude hap_agent, acme, …). */
export function userFacingStats(stats: StatsSummary | null | undefined): {
  active_sessions: number | null
  backends_up: number | null
  backends: NonNullable<StatsSummary['backends']>
} {
  if (!stats) {
    return { active_sessions: null, backends_up: null, backends: [] }
  }
  const backends = (stats.backends ?? []).filter((b) => !isInternalBackend(b.name))
  const active_sessions = backends.reduce((sum, b) => sum + (Number(b.sessions) || 0), 0)
  const backends_up = backends.filter((b) => String(b.status).toUpperCase() === 'UP').length
  return { active_sessions, backends_up, backends }
}

export function flattenBackends(data: unknown): BackendServer[] {
  if (Array.isArray(data)) return filterUserBackends(data as BackendServer[])
  if (!data || typeof data !== 'object') return []
  const obj = data as Record<string, unknown>
  if (Array.isArray(obj.servers)) {
    return filterUserBackends(obj.servers as BackendServer[])
  }
  if (Array.isArray(obj.backends)) {
    const groups = obj.backends as Array<Record<string, unknown>>
    const out: BackendServer[] = []
    for (const g of groups) {
      const backendName = String(g.name ?? g.backend ?? '')
      if (isInternalBackend(backendName)) continue
      const servers = Array.isArray(g.servers) ? (g.servers as BackendServer[]) : []
      if (servers.length === 0 && g.name && g.address) {
        out.push(g as unknown as BackendServer)
        continue
      }
      for (const s of servers) {
        const be = String(s.backend || backendName)
        if (isInternalBackend(be)) continue
        out.push({
          ...s,
          backend: be,
        })
      }
    }
    return out
  }
  return []
}

const TOKEN_KEY = 'hapanel_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function api<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
  })

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { raw: text }
    }
  }

  if (!res.ok) {
    const raw =
      data && typeof data === 'object' && data !== null && 'error' in data
        ? String((data as { error: unknown }).error)
        : res.statusText || 'request failed'
    throw new ApiError(res.status, translateError(raw))
  }
  return data as T
}
