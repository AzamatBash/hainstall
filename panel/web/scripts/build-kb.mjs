#!/usr/bin/env node
/**
 * Embed vpn-hub articles into src/kb/bundle.json for the panel UI.
 * Source: KB_SRC env or ../../kb relative to panel/web.
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

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
    // Section heading: non-empty line that is not an article bullet
    if (!trimmed.startsWith('-')) {
      current = { title: trimmed, articles: [] }
      sections.push(current)
    }
  }
  return sections.filter((s) => s.articles.length > 0)
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
    const t = para.join('\n').trimEnd()
    if (t.trim()) blocks.push({ type: 'p', text: t })
    para = []
  }

  for (let i = start; i < lines.length; i++) {
    const line = lines[i]
    const tag = line.trim()
    if (LANGS.has(tag)) {
      flushPara()
      i++
      const codeLines = []
      while (i < lines.length && lines[i].trim() !== '') {
        codeLines.push(lines[i])
        i++
      }
      blocks.push({ type: 'code', lang: tag, code: codeLines.join('\n') })
      // i points at blank or past end; for-loop will i++
      continue
    }
    if (line.trim() === '') {
      flushPara()
      continue
    }
    para.push(line)
  }
  flushPara()

  const bodyText = blocks
    .map((b) => (b.type === 'p' ? b.text : b.code))
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
    // Prefer catalog title from index (same content, consistent nav)
    parsed.title = a.title
    parsed.fields = detectFields(parsed.bodyText + '\n' + parsed.blocks.map((b) => (b.type === 'code' ? b.code : b.text)).join('\n'))
    articles[a.path] = parsed
  }
}

const bundle = {
  generatedAt: new Date().toISOString(),
  source: kbSrc,
  sections,
  articles,
}

fs.mkdirSync(path.dirname(outFile), { recursive: true })
fs.writeFileSync(outFile, JSON.stringify(bundle))
console.log(
  `KB bundle: ${Object.keys(articles).length} articles, ${sections.length} sections → ${path.relative(webRoot, outFile)}`,
)
