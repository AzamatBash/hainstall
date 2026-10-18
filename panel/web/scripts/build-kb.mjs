#!/usr/bin/env node
/**
 * Embed vpn-hub articles into src/kb/bundle.json for the panel UI.
 * Source: KB_SRC env or ../../kb relative to panel/web.
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { normalizeProse, prettyCode } from './format-kb.mjs'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(__dirname, '..')
const defaultSrc = path.resolve(webRoot, '../kb')
const kbSrc = process.env.KB_SRC ? path.resolve(process.env.KB_SRC) : defaultSrc
const outFile = path.join(webRoot, 'src/kb/bundle.json')

const LANGS = new Set([
  'bash',
  'nginx',
  'js',
  'yaml',
  'json',
  'text',
  'ini',
  'toml',
  'sh',
  'python',
  'sql',
  'xml',
  'html',
  'css',
  'env',
  'unit',
])

const SKIP_ALIAS = new Set([
  'ip',
  'tcp',
  'udp',
  'fp',
  'ss',
  'ru',
  'de',
  'nl',
  'fi',
  'host', // too common in configs; keep «Хост»
  'rw',
])

function slugify(s) {
  return s
    .toLowerCase()
    .replace(/[«»"'()]/g, '')
    .replace(/[^a-z0-9а-яё]+/gi, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80)
}

function parseIndex(raw) {
  const lines = raw.replace(/^\uFEFF/, '').split(/\r?\n/)
  const sections = []
  let current = null
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('Заголовок:')) continue
    const article = trimmed.match(/^-\s+(.+?)\s+\((a\/[^)]+\.txt)\)\s*$/)
    if (article) {
      if (!current) {
        current = { title: 'Разное', articles: [] }
        sections.push(current)
      }
      current.articles.push({ title: article[1], path: article[2] })
      continue
    }
    if (!trimmed.startsWith('-')) {
      current = { title: trimmed, articles: [] }
      sections.push(current)
    }
  }
  return sections.filter((s) => s.articles.length > 0)
}

function isCodeyLine(line) {
  const t = line.trim()
  if (!t) return false
  if (/^#/.test(t)) return true // shell / nginx comments
  if (/^[{}\[\]];?$/.test(t)) return true
  if (/^(server|location|upstream|http|events|map|types)\b/i.test(t)) return true
  if (/^(listen|server_name|proxy_|ssl_|root |try_files|return |rewrite )\b/i.test(t)) {
    return true
  }
  if (/^"(tag|listen|port|protocol|settings|streamSettings|inbounds|outbounds)\b/.test(t)) {
    return true
  }
  if (
    /^(apt |yum |dnf |docker |curl |wget |ufw |systemctl |nginx |certbot |mkdir |ln |chmod |chown |echo |cat |tee |export |source |printf |ssh |scp |sed |sysctl |sshd |fail2ban)/i.test(
      t,
    )
  ) {
    return true
  }
  if (/^\$[A-Z_]|^[A-Z_][A-Z0-9_]*=/.test(t)) return true
  return false
}

/** Interactive builder chrome dumped into .txt (not real article text). */
function isBuilderChrome(line) {
  const t = line.trim()
  return /^(Скопировать|Код|ВАШИ КОМАНДЫ|Порт SSH|Открыть порты\b|Роль сервера|Свой пользователь\b|нода \(клиенты|панель или бот)$/i.test(
    t,
  )
}

/** Shell script dumped without a `bash` fence (common in vpn-hub builders). */
function isBareShellScriptStart(line) {
  const t = line.trim()
  return /^#\s*(\d+\.|проверка\b|обновление\b|вход\b|файрвол\b|защита\b|BBR\b)/i.test(t)
}

function isHeadingLine(line) {
  const t = line.trim()
  if (!t || t.length > 90) return false
  if (/^оглавление$/i.test(t)) return false
  if (isBuilderChrome(t)) return false
  if (isCodeyLine(t)) return false
  if (/\t/.test(t)) return false // table row
  if (/^(Шаг|Вариант|Этап)\s+/i.test(t)) return true
  if (
    /^(Если не получилось|Чего .+ не даёт|Разбор по|Итог:|База:|Грабли|Предусловия|Проверка|Соберите свои команды|Настройка за)/i.test(
      t,
    )
  ) {
    return true
  }
  // ALL-CAPS labels (ПЕРВЫЙ ШАГ, ОСНОВНОЙ, …) — not builder chrome
  if (/^[А-ЯЁA-Z0-9][А-ЯЁA-Z0-9\s«»"'/-]{2,40}$/.test(t) && /[А-ЯЁA-Z]{3,}/.test(t)) {
    return true
  }
  // Body lines like "Hysteria2 на UDP — выручает…" are not headings
  if (/\s[—–]\s/.test(t) && t.length > 35) return false
  // Section-like: starts with capital, no sentence end
  if (
    /^[А-ЯЁA-Z«"]/.test(t) &&
    !t.includes(':') &&
    !/[.!?…]$/.test(t) &&
    t.length >= 8 &&
    t.length <= 60 &&
    !/^[-•*⭐🟢🔵🟠🟧🔴✅❌⚠⛔💡📖🔑]/.test(t) &&
    !/^(https?:|curl |sudo |docker |npm |apt )/i.test(t) &&
    !/[→←⟶|{}[\]();=]/.test(t) &&
    !/\d+\.\d+\.\d+\.\d+/.test(t) &&
    /[А-ЯЁа-яё]{3,}/.test(t)
  ) {
    return true
  }
  return false
}

function braceDelta(line) {
  // crude: count braces outside strings
  let d = 0
  let inStr = false
  let q = ''
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (inStr) {
      if (ch === '\\') {
        i++
        continue
      }
      if (ch === q) inStr = false
      continue
    }
    if (ch === '"' || ch === "'") {
      inStr = true
      q = ch
      continue
    }
    if (ch === '{' || ch === '[') d++
    if (ch === '}' || ch === ']') d--
  }
  return d
}

function isTableHeader(line) {
  const parts = line.split('\t')
  return parts.length >= 3 && parts.every((p) => p.trim().length > 0 && p.trim().length < 40)
}

function isTableRow(line) {
  return (line.match(/\t/g) || []).length >= 2
}

function collectCode(lines, startIdx) {
  const codeLines = []
  let i = startIdx
  let depth = 0
  let sawOpen = false
  while (i < lines.length) {
    const line = lines[i]
    const trimmed = line.trim()
    // End of code: blank line
    if (trimmed === '') {
      // Finished a braced block → blank always ends the fence
      if (sawOpen && depth <= 0) break
      if (depth <= 0) {
        let j = i + 1
        while (j < lines.length && lines[j].trim() === '') j++
        if (j >= lines.length) break
        const next = lines[j].trim()
        if (
          LANGS.has(next) ||
          isHeadingLine(next) ||
          (!isCodeyLine(next) && /[А-ЯЁа-яё]/.test(next))
        ) {
          break
        }
      }
      codeLines.push(line)
      i++
      continue
    }
    // Another language marker ends this block if depth is 0
    if (LANGS.has(trimmed) && depth <= 0 && codeLines.some((l) => l.trim())) break

    depth += braceDelta(line)
    if (depth > 0) sawOpen = true
    if (depth < 0) depth = 0
    codeLines.push(line)
    i++
    // closed all braces → stop unless next line continues the config
    if (sawOpen && depth === 0) {
      let j = i
      while (j < lines.length && lines[j].trim() === '') j++
      if (j >= lines.length) break
      const next = lines[j].trim()
      if (
        LANGS.has(next) ||
        isHeadingLine(next) ||
        !isCodeyLine(next)
      ) {
        break
      }
    }
  }
  // trim trailing blank lines inside code
  while (codeLines.length && codeLines[codeLines.length - 1].trim() === '') {
    codeLines.pop()
  }
  return { codeLines, nextIdx: i }
}

function parseArticle(raw, relPath) {
  const text = raw.replace(/^\uFEFF/, '').replace(/\r\n/g, '\n')
  const lines = text.split('\n')
  let title = relPath
  let start = 0
  if (lines[0] && lines[0].startsWith('Заголовок:')) {
    title = lines[0].slice('Заголовок:'.length).trim()
    start = 1
    if (lines[start] === '') start++
  }

  const blocks = []
  let para = []
  const flushPara = () => {
    while (para.length && para[para.length - 1] === '') para.pop()
    const t = normalizeProse(para.join('\n').trimEnd())
    if (t.trim()) blocks.push({ type: 'p', text: t })
    para = []
  }

  for (let i = start; i < lines.length; ) {
    const line = lines[i]
    const tag = line.trim()

    if (LANGS.has(tag)) {
      flushPara()
      const { codeLines, nextIdx } = collectCode(lines, i + 1)
      blocks.push({
        type: 'code',
        lang: tag,
        code: prettyCode(codeLines.join('\n'), tag),
      })
      i = nextIdx
      continue
    }

    // Drop interactive-builder UI leftovers and wiki TOC stubs
    if (isBuilderChrome(tag) || /^оглавление$/i.test(tag)) {
      flushPara()
      i++
      continue
    }

    // Shell script without a language fence: "# 1. …" / "# проверка: …"
    if (isBareShellScriptStart(line)) {
      flushPara()
      const { codeLines, nextIdx } = collectCode(lines, i)
      if (codeLines.length) {
        blocks.push({
          type: 'code',
          lang: 'bash',
          code: prettyCode(codeLines.join('\n'), 'bash'),
        })
      }
      i = nextIdx
      continue
    }

    // Tab-separated troubleshooting / comparison tables
    if (isTableHeader(line) || (isTableRow(line) && para.length === 0)) {
      flushPara()
      const rows = []
      let header = null
      if (isTableHeader(line)) {
        header = line.split('\t').map((c) => c.trim())
        i++
      }
      while (i < lines.length && isTableRow(lines[i])) {
        rows.push(lines[i].split('\t').map((c) => c.trim()))
        i++
      }
      if (rows.length) {
        blocks.push({ type: 'table', header, rows })
      }
      continue
    }

    if (tag === '') {
      flushPara()
      i++
      continue
    }

    // Mid-paragraph section title (e.g. after emoji list without blank line)
    if (isHeadingLine(line)) {
      flushPara()
      blocks.push({ type: 'h', text: tag })
      i++
      continue
    }

    para.push(line)
    i++
  }
  flushPara()

  const bodyText = blocks
    .map((b) => {
      if (b.type === 'code') return b.code
      if (b.type === 'table') {
        return [...(b.header || []), ...b.rows.map((r) => r.join(' '))].join('\n')
      }
      return b.text || ''
    })
    .join('\n')

  return { path: relPath, title, blocks, bodyText }
}

function detectFields(bodyText) {
  const fields = []
  if (
    bodyText.includes('your.domain') ||
    bodyText.includes('node1.your.domain') ||
    bodyText.includes('cdn.your.domain')
  ) {
    fields.push('domain')
  }
  if (bodyText.includes('your-donor.de') || bodyText.includes('your-donor.com')) {
    fields.push('donor')
  }
  if (bodyText.includes('you@example.com')) fields.push('email')
  if (bodyText.includes('PRIVATE_KEY_X25519') || /\bPRIVATE_KEY\b/.test(bodyText)) {
    fields.push('realityPrivate')
  }
  if (bodyText.includes('REALITY_PUBLIC_KEY')) fields.push('realityPublic')
  if (bodyText.includes('REALITY_SHORT_ID')) fields.push('realityShortId')
  if (bodyText.includes('PANEL_IP')) fields.push('panelIp')
  if (bodyText.includes('КЛЮЧ_ИЗ_ПАНЕЛИ')) fields.push('nodeKey')
  return fields
}

function addAlias(set, raw) {
  const a = raw.replace(/^[«"]|[»"]$/g, '').trim()
  if (!a) return
  if (a.length < 2) return
  if (SKIP_ALIAS.has(a.toLowerCase())) return
  // skip very short non-acronym cyrillic noise
  if (a.length === 2 && !/^[A-ZА-ЯЁ0-9]{2}$/u.test(a)) return
  set.add(a)
}

function parseGlossaryTerms(raw) {
  const lines = raw.replace(/^\uFEFF/, '').replace(/\r\n/g, '\n').split('\n')
  const terms = []
  const seenSlug = new Set()
  const seenPrimary = new Set()
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('Заголовок:')) continue
    if (trimmed.startsWith('💡') || trimmed.startsWith('Не нашли') || trimmed === 'Оглавление') {
      continue
    }
    const m = trimmed.match(/^(.+?):\s+(.+)$/)
    if (!m) continue
    const left = m[1].trim()
    const def = m[2].trim()
    if (left.length > 80 || def.length < 20) continue
    // Section titles like "Панель: что с чем связано" — short, no sentence.
    if (def.length < 40 && !/[.!?…]/.test(def)) continue

    const primaryKey = left.toLowerCase()
    if (seenPrimary.has(primaryKey)) continue
    seenPrimary.add(primaryKey)

    const aliases = new Set()
    addAlias(aliases, left)

    // Name (alias1, alias2)
    const paren = left.match(/^(.+?)\s*\((.+)\)\s*$/)
    if (paren) {
      addAlias(aliases, paren[1])
      for (const part of paren[2].split(/[,;/]/)) {
        addAlias(aliases, part.trim())
      }
    }

    // A / B
    if (left.includes(' / ')) {
      for (const part of left.split(/\s*\/\s*/)) addAlias(aliases, part)
    }

    // Emphasize quoted nicknames «грязный»
    for (const q of left.matchAll(/[«"]([^»"]+)[»"]/g)) {
      addAlias(aliases, q[1])
    }

    let slug = slugify(paren ? paren[1] : left.split('/')[0])
    if (!slug) slug = slugify(left)
    if (seenSlug.has(slug)) slug = `${slug}-${terms.length}`
    seenSlug.add(slug)

    const aliasList = [...aliases].sort((a, b) => b.length - a.length)
    terms.push({
      id: slug,
      label: left,
      definition: def,
      aliases: aliasList,
    })
  }
  return terms
}

if (!fs.existsSync(kbSrc)) {
  console.error(`KB source not found: ${kbSrc}`)
  process.exit(1)
}

const indexPath = path.join(kbSrc, 'index.txt')
const sections = parseIndex(fs.readFileSync(indexPath, 'utf8'))
const articles = {}

for (const sec of sections) {
  for (const a of sec.articles) {
    const full = path.join(kbSrc, a.path)
    if (!fs.existsSync(full)) {
      console.warn(`missing ${a.path}`)
      continue
    }
    const parsed = parseArticle(fs.readFileSync(full, 'utf8'), a.path)
    parsed.title = a.title
    const allText =
      parsed.bodyText +
      '\n' +
      parsed.blocks
        .map((b) => {
          if (b.type === 'code') return b.code
          if (b.type === 'table') {
            return [...(b.header || []), ...b.rows.map((r) => r.join(' '))].join('\n')
          }
          return b.text || ''
        })
        .join('\n')
    parsed.fields = detectFields(allText)
    articles[a.path] = parsed
  }
}

const glossaryPath = path.join(kbSrc, 'a/01-glossary.txt')
const glossary = fs.existsSync(glossaryPath)
  ? parseGlossaryTerms(fs.readFileSync(glossaryPath, 'utf8'))
  : []

const bundle = {
  generatedAt: new Date().toISOString(),
  source: kbSrc,
  sections,
  articles,
  glossary,
}

fs.mkdirSync(path.dirname(outFile), { recursive: true })
fs.writeFileSync(outFile, JSON.stringify(bundle))
console.log(
  `KB bundle: ${Object.keys(articles).length} articles, ${sections.length} sections, ${glossary.length} glossary terms → ${path.relative(webRoot, outFile)}`,
)
