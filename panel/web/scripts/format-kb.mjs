/**
 * Display-time / build-time normalizers for KB articles.
 * Source .txt stay as-is; we fix indentation and list breaks in the bundle.
 */

/** Pretty-indent brace languages; preserve // and # comments and strings. */
export function prettyCode(code, lang) {
  const L = (lang || '').toLowerCase()
  if (L === 'text' || L === 'python' || L === 'py') {
    return code.replace(/[ \t]+$/gm, '').trimEnd()
  }
  if (L === 'bash' || L === 'sh' || L === 'caddy') return prettyShell(code)
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
 * Only split jammed enumerations / emoji bullets.
 * Do NOT insert sentence breaks — sources already have paragraphs; CSS wraps the rest.
 */
export function normalizeProse(text) {
  let t = text.replace(/\r\n/g, '\n').replace(/[ \t]+\n/g, '\n')

  // "…: 1. aaa 2. bbb" — only 1–2 digit markers (never years like 2026.)
  // and never split shell comments "# 1. …"
  t = t.replace(/:\s*(?=\d{1,2}\.\s+\S)/g, ':\n')
  t = t.replace(/([^\n#])\s+(\d{1,2}\.\s+)(?=\S)/g, '$1\n$2')

  // Checklist markers jammed on one line / after a sentence
  t = t.replace(/([^\n])\s+(\[(?: |x|X)\]\s+)/g, '$1\n$2')
  t = t.replace(/([.!?…])\s+-\s+(\[(?: |x|X)\])/g, '$1\n$2')

  // "…: - aaa" / "; - bbb" (bullet lists only, not "- [ ]")
  t = t.replace(/:\s*(?=-\s+(?!\[[ xX]?\])\S)/g, ':\n')
  t = t.replace(/;\s*(?=-\s+(?!\[[ xX]?\])\S)/g, '\n')
  t = t.replace(/([.;:])\s+-\s+(?!\[[ xX]?\])(?=\S)/g, '$1\n- ')

  // "а) … б) …"
  t = t.replace(/:\s*(?=[а-яa-z]\)\s)/gi, ':\n')
  t = t.replace(/([^\n])\s+([а-яa-z]\)\s+)/gi, '$1\n$2')

  // Provider bullets jammed on one line (need whitespace before emoji)
  t = t.replace(
    /([^\n\s])\s+(?=[⭐🟢🔵🟠🟧🔴🟡✅❌⛔💡📖🔑⚠])/gu,
    '$1\n',
  )

  // Normalize "- [ ]" → "[ ]"
  t = t.replace(/^-\s+(\[(?: |x|X)\])/gm, '$1')

  t = t.replace(/\n{3,}/g, '\n\n')
  return t.trimEnd()
}
