import { useMemo, useState } from 'react'
import { copyToClipboard } from '../clipboard'
import { highlightCode } from '../kb/highlight'

const LANG_LABEL: Record<string, string> = {
  bash: 'Bash',
  sh: 'Shell',
  nginx: 'Nginx',
  js: 'JSON',
  json: 'JSON',
  yaml: 'YAML',
  text: 'Text',
  env: 'Env',
  ini: 'INI',
  toml: 'TOML',
  python: 'Python',
  sql: 'SQL',
  xml: 'XML',
  html: 'HTML',
  css: 'CSS',
  unit: 'Unit',
}

export default function KbCodeBlock({
  lang,
  code,
}: {
  lang: string
  code: string
}) {
  const [copied, setCopied] = useState(false)
  const lines = useMemo(() => code.split('\n'), [code])
  const segs = useMemo(() => highlightCode(code, lang), [code, lang])
  const label = LANG_LABEL[lang] || lang

  async function onCopy() {
    const ok = await copyToClipboard(code)
    setCopied(ok)
    if (ok) window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <figure className="kb-code">
      <figcaption className="kb-code-bar">
        <span className="kb-code-lang">{label}</span>
        <button
          className={`kb-code-copy${copied ? ' ok' : ''}`}
          type="button"
          onClick={() => void onCopy()}
        >
          {copied ? 'Скопировано' : 'Копировать'}
        </button>
      </figcaption>
      <div className="kb-code-body">
        <div className="kb-code-gutter" aria-hidden>
          {lines.map((_, i) => (
            <span key={i}>{i + 1}</span>
          ))}
        </div>
        <pre className="kb-code-pre">
          <code>
            {segs.map((s, i) =>
              s.c ? (
                <span key={i} className={`kb-tok kb-tok-${s.c}`}>
                  {s.t}
                </span>
              ) : (
                <FragmentPlain key={i} text={s.t} />
              ),
            )}
          </code>
        </pre>
      </div>
    </figure>
  )
}

function FragmentPlain({ text }: { text: string }) {
  return <>{text}</>
}
