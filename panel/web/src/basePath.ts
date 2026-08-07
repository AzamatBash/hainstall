/** Public URL prefix when panel is behind a secret path (e.g. "/a1b2c3d4e5f6"). */
export function basePath(): string {
  if (typeof window === 'undefined') return ''
  const w = (window as unknown as { __HAPANEL_BASE__?: string }).__HAPANEL_BASE__
  if (typeof w === 'string' && w.trim()) {
    const b = w.trim()
    if (b === '/') return ''
    return b.endsWith('/') ? b.slice(0, -1) : b
  }
  return ''
}

/** Prefix an absolute app path with the public base. */
export function withBase(path: string): string {
  const base = basePath()
  if (!path.startsWith('/')) path = '/' + path
  if (!base) return path
  if (path === '/') return base + '/'
  return base + path
}
