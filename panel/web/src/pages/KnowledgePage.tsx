import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, setToken } from '../api'
import { withBase } from '../basePath'
import BrandNav from '../components/BrandNav'
import KbCodeBlock from '../components/KbCodeBlock'
import KbProse from '../components/KbProse'
import bundleJson from '../kb/bundle.json'
import {
  applyKbPlaceholders,
  clearKbValues,
  KB_FIELD_META,
  KbFieldId,
  KbValues,
  loadKbValues,
  saveKbValues,
} from '../kb/placeholders'
import { KbArticle, KbBundle, pathToSlug, slugToPath } from '../kb/types'

const bundle = bundleJson as KbBundle

function logout() {
  void api('/api/auth/logout', { method: 'POST' }).catch(() => {})
  setToken(null)
  window.location.href = withBase('/login')
}

function ArticleView({ article }: { article: KbArticle }) {
  const [values, setValues] = useState<KbValues>(() => loadKbValues())

  useEffect(() => {
    saveKbValues(values)
  }, [values])

  const fields = useMemo(() => {
    const ids = new Set(article.fields as KbFieldId[])
    return KB_FIELD_META.filter((f) => ids.has(f.id))
  }, [article.fields])

  function setField(id: KbFieldId, v: string) {
    setValues((prev) => ({ ...prev, [id]: v }))
  }

  function onReset() {
    clearKbValues()
    setValues({})
  }

  return (
    <article className="kb-article">
      <div className="kb-article-head">
        <Link to="/kb" className="kb-back">
          ← Каталог
        </Link>
        <h1 className="kb-title">{article.title}</h1>
        <p className="muted kb-glossary-line">
          <Link to="/kb/01-glossary">Словарь терминов</Link>
        </p>
      </div>

      {fields.length > 0 && (
        <form
          className="kb-builder"
          onSubmit={(e: FormEvent) => e.preventDefault()}
        >
          <div className="kb-builder-head">
            <strong>Сборщик конфигов</strong>
            <span className="muted">
              Подстановка только в этой вкладке браузера, на сервер не уходит.
            </span>
          </div>
          <div className="kb-builder-grid">
            {fields.map((f) => (
              <label key={f.id} className="kb-field">
                <span>{f.label}</span>
                <input
                  value={values[f.id] || ''}
                  onChange={(e) => setField(f.id, e.target.value)}
                  placeholder={f.placeholder}
                  autoComplete="off"
                  spellCheck={false}
                />
              </label>
            ))}
          </div>
          <div className="row">
            <button className="btn btn-sm" type="button" onClick={onReset}>
              Сбросить
            </button>
          </div>
        </form>
      )}

      <div className="kb-body">
        {article.blocks.map((b, i) => {
          if (b.type === 'code') {
            return (
              <KbCodeBlock
                key={i}
                lang={b.lang}
                code={applyKbPlaceholders(b.code, values)}
              />
            )
          }
          return <KbProse key={i} text={applyKbPlaceholders(b.text, values)} />
        })}
      </div>
    </article>
  )
}

export default function KnowledgePage() {
  const { slug } = useParams()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [sectionOpen, setSectionOpen] = useState<Record<string, boolean>>({})

  const article = useMemo(() => {
    if (!slug) return null
    const path = slugToPath(slug)
    return bundle.articles[path] || null
  }, [slug])

  const filtered = useMemo(() => {
    const query = q.trim().toLowerCase()
    if (!query) return bundle.sections
    return bundle.sections
      .map((sec) => ({
        ...sec,
        articles: sec.articles.filter((a) => {
          const full = bundle.articles[a.path]
          if (!full) return a.title.toLowerCase().includes(query)
          return (
            full.title.toLowerCase().includes(query) ||
            full.bodyText.toLowerCase().includes(query)
          )
        }),
      }))
      .filter((sec) => sec.articles.length > 0)
  }, [q])

  useEffect(() => {
    if (!q.trim()) return
    const next: Record<string, boolean> = {}
    for (const sec of filtered) next[sec.title] = true
    setSectionOpen((prev) => ({ ...prev, ...next }))
  }, [q, filtered])

  return (
    <div className="shell kb-shell">
      <header className="topbar">
        <BrandNav active="kb" />
        <div className="row">
          <button className="btn btn-sm btn-ghost" type="button" onClick={logout}>
            Выйти
          </button>
        </div>
      </header>

      <div className="kb-layout">
        <aside className="kb-nav panel">
          <div className="kb-nav-head">
            <h2 className="kb-nav-title">База знаний</h2>
            <input
              className="kb-search"
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Поиск по заголовку и тексту…"
              aria-label="Поиск по базе знаний"
            />
            <Link to="/kb/01-glossary" className="kb-glossary-nav">
              Словарь терминов
            </Link>
          </div>
          <nav className="kb-sections" aria-label="Разделы базы знаний">
            {filtered.map((sec) => {
              const open = sectionOpen[sec.title] ?? true
              return (
                <div key={sec.title} className="kb-section">
                  <button
                    type="button"
                    className="kb-section-toggle"
                    aria-expanded={open}
                    onClick={() =>
                      setSectionOpen((prev) => ({
                        ...prev,
                        [sec.title]: !open,
                      }))
                    }
                  >
                    <span>{sec.title}</span>
                    <span className="muted">{sec.articles.length}</span>
                  </button>
                  {open && (
                    <ul className="kb-article-list">
                      {sec.articles.map((a) => {
                        const s = pathToSlug(a.path)
                        const active = slug === s
                        return (
                          <li key={a.path}>
                            <Link
                              to={`/kb/${s}`}
                              className={active ? 'active' : undefined}
                              onClick={() => setQ('')}
                            >
                              {a.title}
                            </Link>
                          </li>
                        )
                      })}
                    </ul>
                  )}
                </div>
              )
            })}
            {filtered.length === 0 && (
              <p className="muted">Ничего не найдено.</p>
            )}
          </nav>
        </aside>

        <main className="kb-main panel">
          {slug && !article && (
            <div>
              <p className="error">Статья не найдена.</p>
              <button className="btn" type="button" onClick={() => navigate('/kb')}>
                В каталог
              </button>
            </div>
          )}
          {article && <ArticleView key={article.path} article={article} />}
          {!slug && (
            <div className="kb-home">
              <h1 className="kb-title">База знаний</h1>
              <p className="muted">
                Технические заметки: сервер, Remnawave, протоколы, каскад, CDN,
                балансировка, защита, клиенты. Выберите раздел слева или найдите
                статью поиском.
              </p>
              <p>
                <Link to="/kb/01-glossary">Словарь терминов</Link>
              </p>
              <ul className="kb-home-sections">
                {bundle.sections.map((sec) => (
                  <li key={sec.title}>
                    <strong>{sec.title}</strong>
                    <span className="muted"> — {sec.articles.length} статей</span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </main>
      </div>
    </div>
  )
}
