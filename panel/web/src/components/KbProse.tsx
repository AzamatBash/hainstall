import { Fragment, ReactNode } from 'react'
import { Link } from 'react-router-dom'

function linkifyGlossary(text: string): ReactNode[] {
  const needle = 'Словарь терминов'
  const parts: ReactNode[] = []
  let rest = text
  let key = 0
  while (rest.length) {
    const i = rest.indexOf(needle)
    if (i < 0) {
      parts.push(<Fragment key={key++}>{rest}</Fragment>)
      break
    }
    if (i > 0) parts.push(<Fragment key={key++}>{rest.slice(0, i)}</Fragment>)
    parts.push(
      <Link key={key++} to="/kb/01-glossary" className="kb-glossary-link">
        {needle}
      </Link>,
    )
    rest = rest.slice(i + needle.length)
  }
  return parts
}

export default function KbProse({ text }: { text: string }) {
  const paragraphs = text.split(/\n{2,}/)
  return (
    <div className="kb-prose">
      {paragraphs.map((p, i) => (
        <p key={i}>
          {p.split('\n').map((line, j, arr) => (
            <Fragment key={j}>
              {linkifyGlossary(line)}
              {j < arr.length - 1 ? <br /> : null}
            </Fragment>
          ))}
        </p>
      ))}
    </div>
  )
}
