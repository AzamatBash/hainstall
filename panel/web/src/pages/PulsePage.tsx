import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, translateError, type RemnaPanel } from '../api'

type PulseBucket = {
  key: string
  label: string
  partial?: boolean
  base: number
  inflow: number
  outflow: number
  net: number
  inflow_pct: number
  outflow_pct: number
  net_pct: number
}

type PulsePayload = {
  panel_id: string
  panel_name: string
  active_now: number
  active_by_dates?: number
  disabled_now?: number
  limited_now?: number
  expired_now?: number
  total_users?: number
  users_sampled?: number
  tz: string
  as_of: string
  week_days: PulseBucket[]
  week_total: PulseBucket
  months: PulseBucket[]
  note: string
}

type Tone = 'excellent' | 'ok' | 'weak' | 'bad' | 'neutral'

/** Period scale from VPN benchmarks (RetentionCheck / PureVPN). */
type ScaleKind = 'day' | 'week' | 'month'

function toneOutflow(pct: number, kind: ScaleKind): Tone {
  const t =
    kind === 'month'
      ? [0.03, 0.07, 0.1]
      : kind === 'week'
        ? [0.007, 0.016, 0.023]
        : [0.001, 0.0023, 0.0033]
  if (pct < t[0]) return 'excellent'
  if (pct <= t[1]) return 'ok'
  if (pct <= t[2]) return 'weak'
  return 'bad'
}

function toneInflow(pct: number, kind: ScaleKind): Tone {
  const t =
    kind === 'month'
      ? [0.08, 0.05, 0.03]
      : kind === 'week'
        ? [0.019, 0.012, 0.007]
        : [0.0027, 0.0017, 0.001]
  if (pct >= t[0]) return 'excellent'
  if (pct >= t[1]) return 'ok'
  if (pct >= t[2]) return 'weak'
  return 'bad'
}

function toneNet(pct: number, kind: ScaleKind): Tone {
  const hi =
    kind === 'month' ? 0.03 : kind === 'week' ? 0.007 : 0.001
  const lo = -hi
  if (pct >= hi) return 'excellent'
  if (pct >= 0) return 'ok'
  if (pct >= lo) return 'weak'
  return 'bad'
}

function toneNetAbs(n: number): Tone {
  if (n > 0) return 'excellent'
  if (n < 0) return 'bad'
  return 'ok'
}

function fmtInt(n: number): string {
  return new Intl.NumberFormat('ru-RU').format(n)
}

function fmtNet(n: number): string {
  const s = fmtInt(Math.abs(n))
  if (n > 0) return `+${s}`
  if (n < 0) return `−${s}`
  return '0'
}

function fmtPct(n: number, signed = false): string {
  if (!Number.isFinite(n)) return '—'
  const abs = Math.abs(n) * 100
  const digits = abs >= 10 ? 1 : 2
  const body = `${abs.toFixed(digits)}%`
  if (!signed) return body
  if (n > 0) return `+${body}`
  if (n < 0) return `−${body}`
  return '0%'
}

function Cell({
  children,
  tone,
  strong,
}: {
  children: ReactNode
  tone?: Tone
  strong?: boolean
}) {
  return (
    <td className={`pulse-td${tone ? ` pulse-${tone}` : ''}${strong ? ' pulse-strong' : ''}`}>
      {children}
    </td>
  )
}

export default function PulsePage() {
  const [panels, setPanels] = useState<RemnaPanel[]>([])
  const [panelId, setPanelId] = useState('')
  const [data, setData] = useState<PulsePayload | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const loadPanels = useCallback(async () => {
    const res = await api<{ panels: RemnaPanel[] }>('/api/remna-panels')
    const list = Array.isArray(res.panels) ? res.panels : []
    setPanels(list)
    setPanelId((prev) => {
      if (prev && list.some((p) => p.id === prev)) return prev
      return list[0]?.id || ''
    })
  }, [])

  const loadPulse = useCallback(async (id: string) => {
    if (!id) {
      setData(null)
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await api<PulsePayload>(
        `/api/remna-panels/${encodeURIComponent(id)}/pulse?months=5&tz=Europe/Moscow`,
      )
      setData(res)
    } catch (err) {
      setData(null)
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        await loadPanels()
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : translateError(String(err)))
          setLoading(false)
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [loadPanels])

  useEffect(() => {
    if (panelId) void loadPulse(panelId)
    else setLoading(false)
  }, [panelId, loadPulse])

  const weekCols = useMemo(() => {
    if (!data) return []
    return [...data.week_days, data.week_total]
  }, [data])

  const verdict = useMemo(() => {
    if (!data) return '—'
    const t = toneNet(data.week_total.net_pct, 'week')
    const o = toneOutflow(data.week_total.outflow_pct, 'week')
    if (t === 'excellent' && (o === 'excellent' || o === 'ok')) return 'Отлично'
    if (t === 'ok' || t === 'excellent') return 'Нормально'
    if (t === 'weak') return 'Слабо'
    return 'Плохо'
  }, [data])

  return (
    <div className="shell">
      <header className="page-toolbar">
        <div className="page-toolbar-title">
          <h1>Pulse</h1>
        </div>
        <div className="page-toolbar-actions">
          <button
            className="btn btn-sm"
            type="button"
            disabled={!panelId || loading}
            onClick={() => void loadPulse(panelId)}
          >
            Обновить
          </button>
        </div>
      </header>

      <section className="panel pulse-panel">
        <p className="stats-lead">
          База на дату = createdAt &lt; T и expireAt &gt; T · приток/отток по этим же датам · DISABLED
          вне базы · неделя со сдвигом, месяцы календарные
        </p>

        {error && <p className="error">{error}</p>}

        {panels.length === 0 && !loading ? (
          <p className="muted">
            Панелей Remnawave нет — добавьте на <Link to="/nodes">нодах</Link>.
          </p>
        ) : (
          <>
            <div className="stats-panel-picker" role="group" aria-label="Панель Remnawave">
              <div className="stats-panel-picker-list">
                {panels.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    className={`stats-panel-chip${panelId === p.id ? ' active' : ''}`}
                    onClick={() => setPanelId(p.id)}
                  >
                    {p.name}
                  </button>
                ))}
              </div>
            </div>

            {loading && (
              <p className="muted">Грузим пользователей Remnawave и считаем базу по датам (первый раз может занять минуту)…</p>
            )}

            {data && !loading && (
              <>
                <div className="pulse-kpis" aria-label="Итог 7 дней">
                  <div className="pulse-kpi">
                    <div className="pulse-kpi-label">Приток · 7д</div>
                    <div className="pulse-kpi-value">
                      {fmtInt(data.week_total.inflow)}{' '}
                      <span className={`pulse-kpi-pct pulse-${toneInflow(data.week_total.inflow_pct, 'week')}`}>
                        {fmtPct(data.week_total.inflow_pct)}
                      </span>
                    </div>
                    <div className="pulse-kpi-sub">% от базы на старт окна · норма нед. 1.2–1.9%</div>
                  </div>
                  <div className="pulse-kpi">
                    <div className="pulse-kpi-label">Отток · 7д</div>
                    <div className="pulse-kpi-value">
                      {fmtInt(data.week_total.outflow)}{' '}
                      <span className={`pulse-kpi-pct pulse-${toneOutflow(data.week_total.outflow_pct, 'week')}`}>
                        {fmtPct(data.week_total.outflow_pct)}
                      </span>
                    </div>
                    <div className="pulse-kpi-sub">expireAt за 7д · норма нед. 0.7–1.6%</div>
                  </div>
                  <div className="pulse-kpi">
                    <div className="pulse-kpi-label">Чистый · 7д</div>
                    <div className={`pulse-kpi-value pulse-${toneNet(data.week_total.net_pct, 'week')}`}>
                      {fmtNet(data.week_total.net)}{' '}
                      <span className="pulse-kpi-pct">{fmtPct(data.week_total.net_pct, true)}</span>
                    </div>
                    <div className="pulse-kpi-sub">% от базы старта окна · норма 0…+0.7%</div>
                  </div>
                  <div className="pulse-kpi">
                    <div className="pulse-kpi-label">База на старт 7д</div>
                    <div className="pulse-kpi-value">{fmtInt(data.week_total.base)}</div>
                    <div className="pulse-kpi-sub">оценка: {verdict}</div>
                  </div>
                </div>

                <p className="muted pulse-status-line">
                  Remnawave сейчас: ACTIVE {fmtInt(data.active_now)}
                  {data.active_by_dates != null ? ` · по датам сейчас ${fmtInt(data.active_by_dates)}` : ''}
                  {data.disabled_now != null ? ` · DISABLED ${fmtInt(data.disabled_now)}` : ''}
                  {data.limited_now != null ? ` · LIMITED ${fmtInt(data.limited_now)}` : ''}
                  {data.expired_now != null ? ` · EXPIRED ${fmtInt(data.expired_now)}` : ''}
                  {data.users_sampled != null ? ` · юзеров в расчёте ${fmtInt(data.users_sampled)}` : ''}
                  {` · ${data.tz}`}
                </p>

                <h2 className="pulse-section-title">Неделя · 7 дней со сдвигом</h2>
                <div className="table-wrap pulse-table-wrap">
                  <table className="pulse-table">
                    <thead>
                      <tr>
                        <th>Метрика</th>
                        {weekCols.map((c) => (
                          <th key={c.key} className={c.key === '7d' ? 'pulse-total-col' : undefined}>
                            {c.label}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      <tr>
                        <th scope="row">База на начало</th>
                        {weekCols.map((c) => (
                          <Cell key={c.key}>{fmtInt(c.base)}</Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Приток, шт</th>
                        {weekCols.map((c) => (
                          <Cell key={c.key}>{fmtInt(c.inflow)}</Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Отток, шт</th>
                        {weekCols.map((c) => (
                          <Cell key={c.key}>{fmtInt(c.outflow)}</Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Чистый, шт</th>
                        {weekCols.map((c) => (
                          <Cell key={c.key} tone={toneNetAbs(c.net)} strong>
                            {fmtNet(c.net)}
                          </Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Приток, %</th>
                        {weekCols.map((c) => (
                          <Cell
                            key={c.key}
                            tone={toneInflow(c.inflow_pct, c.key === '7d' ? 'week' : 'day')}
                            strong
                          >
                            {fmtPct(c.inflow_pct)}
                          </Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Отток (churn), %</th>
                        {weekCols.map((c) => (
                          <Cell
                            key={c.key}
                            tone={toneOutflow(c.outflow_pct, c.key === '7d' ? 'week' : 'day')}
                            strong
                          >
                            {fmtPct(c.outflow_pct)}
                          </Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Чистый, %</th>
                        {weekCols.map((c) => (
                          <Cell
                            key={c.key}
                            tone={toneNet(c.net_pct, c.key === '7d' ? 'week' : 'day')}
                            strong
                          >
                            {fmtPct(c.net_pct, true)}
                          </Cell>
                        ))}
                      </tr>
                    </tbody>
                  </table>
                </div>

                <h2 className="pulse-section-title">Месяцы · календарь без сдвига</h2>
                <div className="table-wrap pulse-table-wrap">
                  <table className="pulse-table">
                    <thead>
                      <tr>
                        <th>Метрика</th>
                        {data.months.map((m) => (
                          <th key={m.key}>{m.label}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      <tr>
                        <th scope="row">База на начало</th>
                        {data.months.map((m) => (
                          <Cell key={m.key}>{fmtInt(m.base)}</Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Приток, шт</th>
                        {data.months.map((m) => (
                          <Cell key={m.key}>{fmtInt(m.inflow)}</Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Отток, шт</th>
                        {data.months.map((m) => (
                          <Cell key={m.key}>{fmtInt(m.outflow)}</Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Чистый, шт</th>
                        {data.months.map((m) => (
                          <Cell key={m.key} tone={toneNetAbs(m.net)} strong>
                            {fmtNet(m.net)}
                          </Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Приток, %</th>
                        {data.months.map((m) => (
                          <Cell
                            key={m.key}
                            tone={m.partial ? 'neutral' : toneInflow(m.inflow_pct, 'month')}
                            strong
                          >
                            {fmtPct(m.inflow_pct)}
                          </Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Отток (churn), %</th>
                        {data.months.map((m) => (
                          <Cell
                            key={m.key}
                            tone={m.partial ? 'neutral' : toneOutflow(m.outflow_pct, 'month')}
                            strong
                          >
                            {fmtPct(m.outflow_pct)}
                          </Cell>
                        ))}
                      </tr>
                      <tr>
                        <th scope="row">Чистый, %</th>
                        {data.months.map((m) => (
                          <Cell
                            key={m.key}
                            tone={m.partial ? 'neutral' : toneNet(m.net_pct, 'month')}
                            strong
                          >
                            {fmtPct(m.net_pct, true)}
                          </Cell>
                        ))}
                      </tr>
                    </tbody>
                  </table>
                </div>

                <aside className="pulse-legend" aria-label="Шкала VPN">
                  <h3>Шкала VPN · месяц</h3>
                  <div className="pulse-legend-grid">
                    <div className="pulse-legend-row pulse-excellent">
                      <strong>Отлично</strong>
                      <span>отток &lt;3%</span>
                      <span>приток ≥8%</span>
                      <span>чистый ≥+3%</span>
                    </div>
                    <div className="pulse-legend-row pulse-ok">
                      <strong>Нормально</strong>
                      <span>отток 3–7%</span>
                      <span>приток 5–8%</span>
                      <span>чистый 0…+3%</span>
                    </div>
                    <div className="pulse-legend-row pulse-weak">
                      <strong>Слабо</strong>
                      <span>отток 7–10%</span>
                      <span>приток 3–5%</span>
                      <span>чистый −3…0%</span>
                    </div>
                    <div className="pulse-legend-row pulse-bad">
                      <strong>Плохо</strong>
                      <span>отток &gt;10%</span>
                      <span>приток &lt;3%</span>
                      <span>чистый &lt;−3%</span>
                    </div>
                  </div>
                  <p className="muted pulse-note">
                    Avg churn VPN ~5.5%/мес · solid B2C &lt;5%. Неделя ≈ ÷4.3, день ≈ ÷30. Неполный
                    месяц без месячной окраски %. База каждого дня/месяца своя — по expireAt/createdAt.
                    {data.note}
                  </p>
                </aside>
              </>
            )}
          </>
        )}
      </section>
    </div>
  )
}
