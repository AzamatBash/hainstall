/** Lightweight token highlight for KB code blocks (no deps). */

type Seg = { t: string; c?: 'cmt' | 'str' | 'key' | 'num' | 'pun' }

export function highlightCode(code: string, lang: string): Seg[] {
  const L = lang.toLowerCase()
  if (L === 'json' || L === 'js') return highlightJsonish(code)
  if (L === 'bash' || L === 'sh') return highlightShell(code)
  if (L === 'nginx' || L === 'yaml' || L === 'env' || L === 'ini' || L === 'toml') {
    return highlightHashComments(code)
  }
  return [{ t: code }]
}

function pushPlain(out: Seg[], s: string) {
  if (s) out.push({ t: s })
}

function highlightShell(code: string): Seg[] {
  const out: Seg[] = []
  const lines = code.split('\n')
  lines.forEach((line, li) => {
    if (li) out.push({ t: '\n' })
    const hash = line.indexOf('#')
    // keep # inside strings roughly: only treat as comment if not in quotes
    if (hash >= 0 && !/['"`]/.test(line.slice(0, hash))) {
      pushPlain(out, line.slice(0, hash))
      out.push({ t: line.slice(hash), c: 'cmt' })
      return
    }
    // crude string + number
    let i = 0
    while (i < line.length) {
      const ch = line[i]
      if (ch === "'" || ch === '"' || ch === '`') {
        const q = ch
        let j = i + 1
        while (j < line.length && line[j] !== q) {
          if (line[j] === '\\') j++
          j++
        }
        j = Math.min(j + 1, line.length)
        out.push({ t: line.slice(i, j), c: 'str' })
        i = j
        continue
      }
      if (/[0-9]/.test(ch)) {
        let j = i + 1
        while (j < line.length && /[0-9.]/.test(line[j])) j++
        out.push({ t: line.slice(i, j), c: 'num' })
        i = j
        continue
      }
      let j = i + 1
      while (j < line.length && !/['"`#0-9]/.test(line[j])) j++
      pushPlain(out, line.slice(i, j))
      i = j
    }
  })
  return out
}

function highlightHashComments(code: string): Seg[] {
  const out: Seg[] = []
  const lines = code.split('\n')
  lines.forEach((line, li) => {
    if (li) out.push({ t: '\n' })
    const hash = line.indexOf('#')
    if (hash >= 0) {
      pushPlain(out, line.slice(0, hash))
      out.push({ t: line.slice(hash), c: 'cmt' })
    } else {
      // nginx directives / yaml keys
      const m = line.match(/^(\s*)([\w.-]+)(\s+)/)
      if (m && !line.trimStart().startsWith('-')) {
        pushPlain(out, m[1])
        out.push({ t: m[2], c: 'key' })
        pushPlain(out, line.slice(m[0].length - m[3].length))
      } else {
        pushPlain(out, line)
      }
    }
  })
  return out
}

function highlightJsonish(code: string): Seg[] {
  const out: Seg[] = []
  let i = 0
  while (i < code.length) {
    const ch = code[i]
    if (ch === '/' && code[i + 1] === '/') {
      let j = i + 2
      while (j < code.length && code[j] !== '\n') j++
      out.push({ t: code.slice(i, j), c: 'cmt' })
      i = j
      continue
    }
    if (ch === '"' || ch === "'") {
      const q = ch
      let j = i + 1
      while (j < code.length && code[j] !== q) {
        if (code[j] === '\\') j++
        j++
      }
      j = Math.min(j + 1, code.length)
      // key if followed by :
      let k = j
      while (k < code.length && /\s/.test(code[k])) k++
      const isKey = code[k] === ':'
      out.push({ t: code.slice(i, j), c: isKey ? 'key' : 'str' })
      i = j
      continue
    }
    if (/[0-9]/.test(ch) || (ch === '-' && /[0-9]/.test(code[i + 1] || ''))) {
      let j = i + 1
      while (j < code.length && /[0-9.eE+-]/.test(code[j])) j++
      out.push({ t: code.slice(i, j), c: 'num' })
      i = j
      continue
    }
    if (/[{}\[\]:,]/.test(ch)) {
      out.push({ t: ch, c: 'pun' })
      i++
      continue
    }
    let j = i + 1
    while (j < code.length && !/["'`\/0-9{}\[\]:,-]/.test(code[j])) j++
    // bool/null
    const word = code.slice(i, j)
    if (/^(true|false|null)\b/.test(word)) {
      out.push({ t: word, c: 'num' })
    } else {
      pushPlain(out, word)
    }
    i = j
  }
  return out
}
