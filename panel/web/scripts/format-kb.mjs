/**
 * Display-time / build-time normalizers for KB articles.
 * Source .txt stay as-is; we fix indentation and list breaks in the bundle.
 */

/** Pretty-indent brace languages; preserve // and # comments and strings. */
export function prettyCode(code, lang) {
  const L = (lang || '').toLowerCase()
  if (L === 'text') return code.replace(/[ \t]+$/gm, '').trimEnd()
  if (L === 'bash' || L === 'sh') return prettyShell(code)
  if (L === 'yaml' || L === 'env' || L === 'ini' || L === 'toml') {
    return prettySoftIndent(code, 2)
  }
  return prettyBraces(code, {
    step: L === 'nginx' ? 4 : 2,
    breakOnSemicolon: L === 'nginx' || L === 'css',
  })
}

function prettyShell(code) {
  return code
    .replace(/\r\n/g, '\n')
    .split('\n')
    .map((l) => l.replace(/[ \t]+$/g, ''))
    .join('\n')
    .trimEnd()
}

function prettySoftIndent(code, step) {
  const lines = code.replace(/\r\n/g, '\n').split('\n')
  let depth = 0
  const out = []
  for (const raw of lines) {
    const trimmed = raw.replace(/[ \t]+$/g, '').trim()
    if (!trimmed) {
      if (out.length && out[out.length - 1] !== '') out.push('')
      continue
    }
    if (/^[}\])]/.test(trimmed)) depth = Math.max(0, depth - 1)
    out.push(' '.repeat(depth * step) + trimmed)
    if (/[{(\[]$/.test(trimmed) || /:\s*[|{[]\s*$/.test(trimmed)) depth++
  }
  return out.join('\n').replace(/\n{3,}/g, '\n\n').trimEnd()
}

/**
 * Structural pretty-printer. Ignores original newlines (sources are flat)
 * and rebuilds indentation from braces / brackets / commas.
 */
function prettyBraces(code, opts = {}) {
  const step = opts.step ?? 2
  const breakOnSemicolon = !!opts.breakOnSemicolon
  const tokens = tokenize(code.replace(/\r\n/g, '\n'), { semicolon: breakOnSemicolon })
  const out = []
  let indent = 0
  let buf = ''

  const emit = (s) => {
    out.push(' '.repeat(indent * step) + s)
  }
  const flushBuf = () => {
    const t = buf.trim()
    buf = ''
    if (t) emit(t)
  }

  for (let i = 0; i < tokens.length; i++) {
    const tok = tokens[i]
    if (tok.type === 'nl' || tok.type === 'ws') {
      if (buf && !buf.endsWith(' ')) buf += ' '
      continue
    }
    if (tok.type === 'cmt') {
      // keep trailing comments on the same directive when possible
      if (breakOnSemicolon && buf.trim()) {
        buf += '  ' + tok.value.trim()
        flushBuf()
      } else {
        flushBuf()
        emit(tok.value.trim())
      }
      continue
    }
    const v = tok.value
    if (v === '{' || v === '[') {
      buf += v
      flushBuf()
      indent++
      continue
    }
    if (v === '}' || v === ']') {
      flushBuf()
      indent = Math.max(0, indent - 1)
      let chunk = v
      while (i + 1 < tokens.length) {
        const n = tokens[i + 1]
        if (n.type === 'ws' || n.type === 'nl') {
          i++
          continue
        }
        if (n.type === 'other' && n.value === ',') {
          chunk += ','
          i++
        }
        break
      }
      emit(chunk)
      continue
    }
    if (v === ',') {
      buf += ','
      flushBuf()
      continue
    }
    if (v === ';') {
      buf += ';'
      // keep "directive; # comment" on one line
      let j = i + 1
      while (j < tokens.length && (tokens[j].type === 'ws' || tokens[j].type === 'nl')) j++
      if (j < tokens.length && tokens[j].type === 'cmt') {
        buf += '  ' + tokens[j].value.trim()
        i = j
      }
      flushBuf()
      continue
    }
    buf += v
  }
  flushBuf()
  return out.join('\n').trimEnd()
}

function tokenize(src, opts = {}) {
  const stop = opts.semicolon ? '{}[],;' : '{}[],'
  const tokens = []
  let i = 0
  while (i < src.length) {
    const ch = src[i]
    if (ch === '\n') {
      tokens.push({ type: 'nl', value: '\n' })
      i++
      continue
    }
    if (ch === ' ' || ch === '\t') {
      let j = i + 1
      while (j < src.length && (src[j] === ' ' || src[j] === '\t')) j++
      tokens.push({ type: 'ws', value: src.slice(i, j) })
      i = j
      continue
    }
    // // comments, but not URL schemes (http://, https://)
    if (ch === '/' && src[i + 1] === '/' && !(i > 0 && src[i - 1] === ':')) {
      let j = i + 2
      while (j < src.length && src[j] !== '\n') j++
      tokens.push({ type: 'cmt', value: src.slice(i, j) })
      i = j
      continue
    }
    if (ch === '#') {
      let j = i + 1
      while (j < src.length && src[j] !== '\n') j++
      tokens.push({ type: 'cmt', value: src.slice(i, j) })
      i = j
      continue
    }
    if (ch === '"' || ch === "'") {
      const q = ch
      let j = i + 1
      while (j < src.length && src[j] !== q) {
        if (src[j] === '\\') j++
        j++
      }
      j = Math.min(j + 1, src.length)
      tokens.push({ type: 'str', value: src.slice(i, j) })
      i = j
      continue
    }
    if (stop.includes(ch)) {
      tokens.push({ type: 'other', value: ch })
      i++
      continue
    }
    let j = i + 1
    while (
      j < src.length &&
      src[j] !== '\n' &&
      src[j] !== ' ' &&
      src[j] !== '\t' &&
      src[j] !== '"' &&
      src[j] !== "'" &&
      src[j] !== '#' &&
      !(
        src[j] === '/' &&
        src[j + 1] === '/' &&
        !(j > 0 && src[j - 1] === ':')
      ) &&
      !stop.includes(src[j])
    ) {
      j++
    }
    tokens.push({ type: 'other', value: src.slice(i, j) })
    i = j
  }
  return tokens
}

/**
 * Break inline numbered / dashed enumerations onto their own lines.
 * Also split very long multi-sentence paragraphs for readability.
 */
export function normalizeProse(text) {
  let t = text.replace(/\r\n/g, '\n').replace(/[ \t]+\n/g, '\n')

  // "…: 1. aaa 2. bbb 3. ccc"
  t = t.replace(/:\s*(?=\d+\.\s)/g, ':\n')
  t = t.replace(/([^\n])\s+(\d+\.\s+)/g, '$1\n$2')

  // "…: - aaa; - bbb"
  t = t.replace(/:\s*(?=-\s)/g, ':\n')
  t = t.replace(/;\s*(?=-\s)/g, '\n')
  t = t.replace(/([.;:])\s+-\s+/g, '$1\n- ')
  t = t.replace(/([^\n])\s+-\s+(?=[А-ЯA-Z«"0-9])/g, '$1\n- ')

  // "а) … б) …"
  t = t.replace(/:\s*(?=[а-яa-z]\)\s)/gi, ':\n')
  t = t.replace(/([^\n])\s+([а-яa-z]\)\s+)/gi, '$1\n$2')

  // Provider bullets jammed: "... . ⭐ Yandex CDN: ..."
  // Require whitespace before emoji so inline (что/✅/❌) stays intact.
  // Use /u so surrogate-pair emoji aren't shredded.
  t = t.replace(
    /([^\n\s])\s+(?=[⭐🟢🔵🟠🟧🔴🟡✅❌⛔💡📖🔑⚠])/gu,
    '$1\n',
  )

  // "Term — def. Other — def2" jammed (em dash / en dash)
  t = t.replace(
    /([.!?…»"')\]])\s+(?=[А-ЯA-Z«"][^.\n]{1,50}?\s+[—–]\s+)/g,
    '$1\n',
  )

  t = t
    .split('\n')
    .map((line) => breakLongSentences(line))
    .join('\n')

  t = t
    .split('\n')
    .map((line) => breakSemicolonList(line))
    .join('\n')

  t = t.replace(/\n{3,}/g, '\n\n')
  return t.trimEnd()
}

function breakLongSentences(line) {
  if (line.length < 160) return line
  const t = line.trim()
  if (/^[-•*\d]/.test(t)) return line
  if (/^[⭐🟢🔵🟠🟧🔴🟡✅❌⛔💡📖🔑⚠]/u.test(t)) return line
  if (/https?:\/\//.test(line) && line.length < 220) return line
  return line.replace(/([.!?…])\s+(?=[A-ZА-ЯЁ«"0-9])/g, '$1\n\n')
}

function breakSemicolonList(line) {
  if (line.length < 120) return line
  const parts = line.split(/;\s+/)
  if (parts.length < 3) return line
  const ok = parts.every((p) => p.length >= 12 && p.length <= 180)
  if (!ok) return line
  return parts.join('\n')
}
