import type { BackendServer } from './api'

const CACHE_KEY = 'hapanel_node_cache_v1'

export type NodeLiveMetrics = {
  cpu: number
  loadAvg?: number[]
  downBps: number | null
  upBps: number | null
}

export type NodeCacheEntry = {
  backends: BackendServer[]
  sessions: string
  metrics?: NodeLiveMetrics
  updatedAt: number
}

type CacheStore = Record<string, NodeCacheEntry>

export type NodeCachePatch = {
  backends?: BackendServer[]
  sessions?: string
  metrics?: NodeLiveMetrics
}

function readAll(): CacheStore {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as CacheStore
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

function writeAll(store: CacheStore) {
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify(store))
  } catch {
    /* quota / private mode */
  }
}

export function getNodeCache(id: string): NodeCacheEntry | null {
  return readAll()[id] ?? null
}

export function getNodesCacheMap(ids: string[]): {
  backends: Record<string, BackendServer[]>
  sessions: Record<string, string>
  metrics: Record<string, NodeLiveMetrics>
} {
  const all = readAll()
  const backends: Record<string, BackendServer[]> = {}
  const sessions: Record<string, string> = {}
  const metrics: Record<string, NodeLiveMetrics> = {}
  for (const id of ids) {
    const e = all[id]
    if (!e) continue
    backends[id] = Array.isArray(e.backends) ? e.backends : []
    if (e.sessions) sessions[id] = e.sessions
    if (e.metrics && typeof e.metrics.cpu === 'number') {
      metrics[id] = e.metrics
    }
  }
  return { backends, sessions, metrics }
}

export function putNodeCache(id: string, patch: NodeCachePatch) {
  const all = readAll()
  const prev = all[id]
  const metrics =
    patch.metrics || prev?.metrics
      ? {
          cpu: patch.metrics?.cpu ?? prev?.metrics?.cpu ?? 0,
          loadAvg: patch.metrics?.loadAvg ?? prev?.metrics?.loadAvg,
          downBps:
            patch.metrics?.downBps != null
              ? patch.metrics.downBps
              : (prev?.metrics?.downBps ?? null),
          upBps:
            patch.metrics?.upBps != null
              ? patch.metrics.upBps
              : (prev?.metrics?.upBps ?? null),
        }
      : undefined
  all[id] = {
    backends: patch.backends ?? prev?.backends ?? [],
    sessions: patch.sessions ?? prev?.sessions ?? '—',
    metrics,
    updatedAt: Date.now(),
  }
  writeAll(all)
}

export function putNodesCache(entries: Array<{ id: string } & NodeCachePatch>) {
  const all = readAll()
  const now = Date.now()
  for (const e of entries) {
    const prev = all[e.id]
    const metrics =
      e.metrics || prev?.metrics
        ? {
            cpu: e.metrics?.cpu ?? prev?.metrics?.cpu ?? 0,
            loadAvg: e.metrics?.loadAvg ?? prev?.metrics?.loadAvg,
            downBps:
              e.metrics?.downBps != null
                ? e.metrics.downBps
                : (prev?.metrics?.downBps ?? null),
            upBps:
              e.metrics?.upBps != null
                ? e.metrics.upBps
                : (prev?.metrics?.upBps ?? null),
          }
        : undefined
    all[e.id] = {
      backends: e.backends ?? prev?.backends ?? [],
      sessions: e.sessions ?? prev?.sessions ?? '—',
      metrics,
      updatedAt: now,
    }
  }
  writeAll(all)
}

export function removeNodeCache(id: string) {
  const all = readAll()
  if (!(id in all)) return
  delete all[id]
  writeAll(all)
}
