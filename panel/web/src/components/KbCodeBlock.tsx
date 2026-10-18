import { useState } from 'react'
import { copyToClipboard } from '../clipboard'

export default function KbCodeBlock({
  lang,
  code,
}: {
  lang: string
  code: string
}) {
  const [copied, setCopied] = useState(false)

  async function onCopy() {
    const ok = await copyToClipboard(code)
    setCopied(ok)
    if (ok) window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className="kb-code">
      <div className="kb-code-bar">
        <span className="kb-code-lang">{lang}</span>
        <button className="btn btn-sm" type="button" onClick={() => void onCopy()}>
          {copied ? 'Скопировано' : 'Копировать'}
        </button>
      </div>
      <pre>
        <code>{code}</code>
      </pre>
    </div>
  )
}
