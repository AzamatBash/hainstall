import { Fragment, ReactNode, useMemo } from 'react'
import { Link } from 'react-router-dom'
import {
  buildGlossaryMatcher,
  GLOSSARY_PATH,
  glossaryHref,
} from '../kb/glossary'
import type { KbGlossaryTerm } from '../kb/types'

function linkifyLine(
  text: string,
  findMatches: ReturnType<typeof buildGlossaryMatcher>,
  usedIds: Set<string>,
  keyBase: number,
): ReactNode[] {
  const parts: ReactNode[] = []
  let key = keyBase

  // First pass: split out "Словарь терминов"
  const dict = 'Словарь терминов'
  type Piece = { kind: 'text' | 'dict'; text: string }
  const pieces: Piece[] = []
  let rest = text
  while (rest.length) {
    const i = rest.indexOf(dict)
    if (i < 0) {
      pieces.push({ kind: 'text', text: rest })
      break
    }
    if (i > 0) pieces.push({ kind: 'text', text: rest.slice(0, i) })
    pieces.push({ kind: 'dict', text: dict })
    rest = rest.slice(i + dict.length)
  }

  for (const piece of pieces) {
    if (piece.kind === 'dict') {
      parts.push(
        <Link key={key++} to={GLOSSARY_PATH} className="kb-wiki-link">
          {piece.text}
        </Link>,
      )
      continue
    }
    const matches = findMatches(piece.text, usedIds)
    let cursor = 0
    for (const m of matches) {
      if (m.start > cursor) {
        parts.push(<Fragment key={key++}>{piece.text.slice(cursor, m.start)}</Fragment>)
      }
      parts.push(
        <Link
          key={key++}
          to={glossaryHref(m.id)}
          className="kb-term-link"
          title="Словарь терминов"
        >
          {m.text}
        </Link>,
      )
      cursor = m.end
    }
    if (cursor < piece.text.length) {
      parts.push(<Fragment key={key++}>{piece.text.slice(cursor)}</Fragment>)
    }
  }
  return parts
}

function isDefLine(line: string): boolean {
  const m = line.match(/^(.+?):\s+(.+)$/)
  if (!m) return false
  return m[1].length <= 80 && m[2].length >= 8
}

export default function KbProse({
  text,
  glossary,
  usedTermIds,
  isGlossary,
}: {
  text: string
  glossary: KbGlossaryTerm[]
  usedTermIds: Set<string>
  isGlossary?: boolean
}) {
  const findMatches = useMemo(() => buildGlossaryMatcher(glossary), [glossary])

  // Glossary page: definition list
  if (isGlossary) {
    const lines = text.split('\n').map((l) => l.trimEnd())
    const defs = lines.filter((l) => l.trim() && isDefLine(l.trim()))
    if (defs.length >= 2 && defs.length >= Math.floor(lines.filter((l) => l.trim()).length * 0.6)) {
      return (
        <dl className="kb-deflist">
          {defs.map((line, i) => {
            const m = line.trim().match(/^(.+?):\s+(.+)$/)!
            const label = m[1].trim()
            const term = glossary.find(
              (t) => t.label === label || t.aliases.some((a) => a === label),
            )
            const id = term?.id
            return (
              <Fragment key={i}>
                <dt id={id}>
                  {id ? (
                    <a href={`#${id}`} className="kb-dt-anchor">
                      {label}
                    </a>
                  ) : (
                    label
                  )}
                </dt>
                <dd>{m[2].trim()}</dd>
              </Fragment>
            )
          })}
        </dl>
      )
    }
  }

  // Tip callouts
  if (/^[💡⚠🚨📖🔑]/.test(text.trim())) {
    return (
      <aside className="kb-callout">
        {text.split('\n').map((line, j, arr) => (
          <Fragment key={j}>
            {linkifyLine(line, findMatches, usedTermIds, j * 100)}
            {j < arr.length - 1 ? <br /> : null}
          </Fragment>
        ))}
      </aside>
    )
  }

  const paragraphs = text.split(/\n{2,}/)
  return (
    <div className="kb-prose">
      {paragraphs.map((p, i) => {
        const lines = p.split('\n').filter((l) => l.trim().length > 0)
        const allDashes = lines.length > 0 && lines.every((l) => /^[-•*]\s+/.test(l.trim()))
        if (allDashes) {
          return (
            <ul key={i} className="kb-list">
              {lines.map((l, j) => (
                <li key={j}>
                  {linkifyLine(
                    l.trim().replace(/^[-•*]\s+/, ''),
                    findMatches,
                    usedTermIds,
                    i * 1000 + j * 10,
                  )}
                </li>
              ))}
            </ul>
          )
        }
        const allNums = lines.length > 0 && lines.every((l) => /^\d+[.)]\s+/.test(l.trim()))
        if (allNums) {
          return (
            <ol key={i} className="kb-list kb-list-num">
              {lines.map((l, j) => (
                <li key={j}>
                  {linkifyLine(
                    l.trim().replace(/^\d+[.)]\s+/, ''),
                    findMatches,
                    usedTermIds,
                    i * 1000 + j * 10,
                  )}
                </li>
              ))}
            </ol>
          )
        }
        // Mixed: intro line + list items → paragraph then list
        const listStart = lines.findIndex(
          (l) => /^[-•*]\s+/.test(l.trim()) || /^\d+[.)]\s+/.test(l.trim()),
        )
        if (listStart > 0) {
          const head = lines.slice(0, listStart)
          const tail = lines.slice(listStart)
          const numbered = tail.every((l) => /^\d+[.)]\s+/.test(l.trim()))
          const ListTag = numbered ? 'ol' : 'ul'
          return (
            <div key={i} className="kb-prose-chunk">
              <p>
                {head.map((line, j, arr) => (
                  <Fragment key={j}>
                    {linkifyLine(line, findMatches, usedTermIds, i * 1000 + j * 10)}
                    {j < arr.length - 1 ? <br /> : null}
                  </Fragment>
                ))}
              </p>
              <ListTag className={`kb-list${numbered ? ' kb-list-num' : ''}`}>
                {tail.map((l, j) => (
                  <li key={j}>
                    {linkifyLine(
                      l.trim().replace(/^([-•*]|\d+[.)])\s+/, ''),
                      findMatches,
                      usedTermIds,
                      i * 2000 + j * 10,
                    )}
                  </li>
                ))}
              </ListTag>
            </div>
          )
        }
        return (
          <p key={i}>
            {lines.map((line, j, arr) => (
              <Fragment key={j}>
                {linkifyLine(line, findMatches, usedTermIds, i * 1000 + j * 10)}
                {j < arr.length - 1 ? <br /> : null}
              </Fragment>
            ))}
          </p>
        )
      })}
    </div>
  )
}
