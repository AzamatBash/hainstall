export type KbBlock =
  | { type: 'p'; text: string }
  | { type: 'h'; text: string }
  | { type: 'code'; lang: string; code: string }

export interface KbArticle {
  path: string
  title: string
  blocks: KbBlock[]
  bodyText: string
  fields: string[]
}

export interface KbSection {
  title: string
  articles: { title: string; path: string }[]
}

export interface KbGlossaryTerm {
  id: string
  label: string
  definition: string
  aliases: string[]
}

export interface KbBundle {
  generatedAt: string
  source: string
  sections: KbSection[]
  articles: Record<string, KbArticle>
  glossary?: KbGlossaryTerm[]
}

export function pathToSlug(p: string): string {
  return p.replace(/^a\//, '').replace(/\.txt$/, '')
}

export function slugToPath(slug: string): string {
  const clean = slug.replace(/^\/+/, '').replace(/\.txt$/, '')
  if (clean.startsWith('a/')) return `${clean}.txt`.replace(/\.txt\.txt$/, '.txt')
  return `a/${clean}.txt`
}
