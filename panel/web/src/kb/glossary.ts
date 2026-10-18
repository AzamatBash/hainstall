import type { KbGlossaryTerm } from './types'

export type GlossaryMatch = { start: number; end: number; id: string; text: string }

const GLOSSARY_PATH = '/kb/01-glossary'

/** Build a matcher that finds longest glossary aliases first. */
export function buildGlossaryMatcher(terms: KbGlossaryTerm[]) {
  const entries: { alias: string; id: string }[] = []
  for (const t of terms) {
    for (const alias of t.aliases) {
      if (alias.length >= 2) entries.push({ alias, id: t.id })
    }
  }
  entries.sort((a, b) => b.alias.length - a.alias.length)

  const escaped = entries.map((e) => ({
    ...e,
    re: new RegExp(
      `(?<![\\p{L}\\p{N}_])${escapeRegExp(e.alias)}(?![\\p{L}\\p{N}_])`,
      'giu',
    ),
  }))

  return function findMatches(text: string, usedIds: Set<string>): GlossaryMatch[] {
    const occupied: boolean[] = new Array(text.length).fill(false)
    const matches: GlossaryMatch[] = []

    for (const { id, re } of escaped) {
      if (usedIds.has(id)) continue
      re.lastIndex = 0
      let m: RegExpExecArray | null
      while ((m = re.exec(text)) !== null) {
        const start = m.index
        const end = start + m[0].length
        let overlap = false
        for (let i = start; i < end; i++) {
          if (occupied[i]) {
            overlap = true
            break
          }
        }
        if (overlap) continue
        for (let i = start; i < end; i++) occupied[i] = true
        matches.push({ start, end, id, text: m[0] })
        usedIds.add(id)
        break // one link per term per article
      }
    }

    matches.sort((a, b) => a.start - b.start)
    return matches
  }
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function glossaryHref(id: string): string {
  return `${GLOSSARY_PATH}#${id}`
}

export { GLOSSARY_PATH }
