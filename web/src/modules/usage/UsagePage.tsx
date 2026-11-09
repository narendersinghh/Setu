// API usage & costs (v1 usage.php): status, free limits, usage, estimated cost, editable prices.
import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ago, num } from '@/lib/format'
import { Alert, Badge, Btn, Card, Field, Hi, Icon, Kpi, Spinner, toast, useInterval, usePageTitle } from '@/components/ui'

interface Row {
  key: string; name: string; kind: string; unit: string; price: number; free: number; per: 'month' | 'day'; url: string; note: string
  group: string; status: [string, string]; used: number; units: number; proj: number; calls: number; in: number; out: number; errors: number
  cost_raw: number; bill: number; bill_proj: number; pct: number | null; has: boolean
}
interface Group { name: string; ic: string; c: string; rows: Row[] }
interface PriceUpdate { status?: 'running' | 'done' | 'error'; msg?: string; changes?: string[]; at?: string }
interface Data {
  month: string; month_name: string; cost: number; proj: number; calls: number; services: number; warn: string[]
  groups: Group[]; daily: { day: string; cost: number; calls: number }[]; models: { id: string; in: number; out: number; in_use: boolean }[]
  fx: { currency: string; symbol: string; rate: number; mode: string; at: string; usd_rate: string }
  price_update: PriceUpdate; web_search: boolean
}

const UNIT1: Record<string, string> = { searches: 'search', calls: 'call', requests: 'request', messages: 'message', minutes: 'minute', emails: 'email' }
const fmt = (n: number, d = 2) => n.toLocaleString('en-IN', { minimumFractionDigits: d, maximumFractionDigits: d })
const usd = (v: number) => '$' + fmt(v, v < 10 ? 2 : 0)
const trim = (n: number) => String(+n.toFixed(4))
const shortDay = (d: string) => new Date(d + 'T00:00:00').toLocaleDateString('en-IN', { day: '2-digit', month: 'short' })

/** The usage page; `embedded` when shown inside Settings (no page title). */
export default function UsagePage({ embedded }: { embedded?: boolean }) {
  if (!embedded) return <UsageTitled />
  return <Usage />
}

function UsageTitled() {
  usePageTitle('API Usage & Costs', 'Every API Setu can use – status, free limits, usage and estimated cost')
  return <Usage />
}

function Usage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['usage'], queryFn: () => api.get<Data>('/usage') })
  const running = q.data?.price_update.status === 'running'
  useInterval(async () => {
    const s = await api.get<PriceUpdate>('/usage/ai-prices').catch(() => null)
    if (s && s.status !== 'running') qc.invalidateQueries({ queryKey: ['usage'] })
  }, running ? 4000 : null)
  const [busy, setBusy] = useState(false)
  if (!q.data) return <Spinner />
  const d = q.data
  const { currency: cur, symbol: sym, rate } = d.fx
  const lc = (v: number) => cur === 'USD' ? '$' + fmt(v) : sym + fmt(v * rate, v * rate < 100 ? 2 : 0)
  const rows = d.groups.flatMap(g => g.rows.map(r => ({ ...r, g })))
  const inuse = rows.filter(r => r.has || r.status[1] === 'green')
  const startAI = async () => {
    setBusy(true)
    try { const r = await api.post('/usage/ai-prices'); toast(r.message) } catch (e: any) { toast(e.message) } finally { setBusy(false); qc.invalidateQueries({ queryKey: ['usage'] }) }
  }
  const pu = d.price_update
  let n = 0
  return <>
    <div className="stats">
      <Kpi label="Estimated bill this month" value={lc(d.cost)} ic="activity" c="violet" foot={usd(d.cost) + ' after free tiers'} />
      <Kpi label="Projected by month end" value={lc(d.proj)} ic="target" c="blue" foot={usd(d.proj) + ' at the current pace'} />
      <Kpi label="API calls this month" value={num(d.calls)} ic="zap" c="teal" foot={d.services + ' services used in ' + d.month_name} />
      <Kpi label="Free limits at risk" value={d.warn.length} ic="alert" c={d.warn.length ? 'rose' : 'green'} foot={d.warn.length ? d.warn.join(', ') : 'all within free limits'} />
    </div>
    {d.warn.length > 0 && <Alert t="warn"><b>Heads-up:</b> at the current pace you will go over the free limit for {d.warn.join(', ')} this month.</Alert>}

    <div className="card-h"><h3><Hi n="activity" c="blue" />Services in use</h3><span className="muted sm">{d.month} · resets on the 1st</span></div>
    {!inuse.length ? <div className="card"><div className="empty sm"><Icon n="activity" /><p>No API is connected or used yet.</p></div></div> :
      <div className="ug">{inuse.map(r => {
        const tok = r.unit === 'tokens'
        const pc = r.free > 0 ? Math.min(100, Math.round(r.used / r.free * 100)) : null
        return <div key={r.key} className={'card uc c-' + r.g.c}>
          <div className="uc-h"><span className="hi"><Icon n={r.g.ic} /></span><div className="grow"><b>{r.name}</b><small>{r.g.name}</small></div><Badge c={r.status[1]}>{r.status[0]}</Badge></div>
          <div className="uc-v"><b>{tok ? (r.has ? fmt((r.in + r.out) / 1000, 1) + 'k' : '0') : fmt(r.used, r.unit === 'minutes' ? 1 : 0)}</b><span>{tok ? 'tokens' : r.unit}{r.per === 'day' ? ' today' : ' this month'}</span></div>
          {pc !== null ? <><div className={'pb ' + (pc >= 100 ? 'r' : pc >= 75 ? 'a' : 'g')}><i style={{ width: pc + '%' }} /></div><small className="pbm">{pc}% of {num(r.free)} free / {r.per}</small></>
            : <small className="pbm">{tok ? 'Billed per token' : r.price ? 'No free tier' : 'Free'}</small>}
          <div className="uc-f"><span>Est. cost <b>{lc(r.bill)}</b></span><span>Projected <b>{lc(r.bill_proj)}</b></span></div>
        </div>
      })}</div>}

    <Chart daily={d.daily} />

    <div className="card">
      <div className="pu"><Hi n="spark" c="violet" />
        <div className="grow"><b>Update prices &amp; free limits with AI</b>
          <small className="muted" style={{ display: 'block' }}>{d.web_search ? "Claude searches the providers' official pricing pages and updates only values that changed." : "Your AI provider cannot search the web – prices come from the model's knowledge. Use Claude for live prices."}{pu.at && !running ? ' Last check: ' + ago(pu.at) + '.' : ''}</small></div>
        <Btn v="p" ic="refresh" busy={busy || running} onClick={startAI}>{running ? 'Checking prices…' : 'Update with AI'}</Btn>
      </div>
      {pu.status && <div className={'al mt mb0 al-' + ({ running: 'info', done: 'ok', error: 'err' }[pu.status] || 'info')}><Icon n={pu.status === 'error' ? 'alert' : 'check'} /><span><b>{pu.msg}</b>{!!pu.changes?.length && <ul className="pu-l">{pu.changes.map((x, i) => <li key={i}>{x}</li>)}</ul>}</span></div>}
    </div>

    <Card p0 title="All APIs" ic="list" c="teal" actions={<span className="muted sm">Status, free limit and price used for estimates</span>}>
      <div className="tw"><table className="tbl"><thead><tr><th className="sn">#</th><th>API</th><th>Status</th><th className="num">Used</th><th className="num">Free limit</th><th className="num">Price (USD)</th><th className="num">Est. cost</th></tr></thead>
        <tbody>{rows.map(r => {
          const tok = r.unit === 'tokens'
          return <tr key={r.key}>
            <td className="sn">{++n}</td>
            <td className="biz" style={{ minWidth: 230 }}><b>{r.name}</b><small>{r.g.name}{r.url && <> · <a className="lnk" href={r.url} target="_blank" rel="noopener noreferrer">pricing</a></>}</small></td>
            <td><Badge c={r.status[1]}>{r.status[0]}</Badge></td>
            <td className="num nowrap">{!r.has ? <span className="muted">0</span> : tok ? fmt(r.in / 1000, 1) + 'k in · ' + fmt(r.out / 1000, 1) + 'k out' : <>{fmt(r.units, r.unit === 'minutes' ? 1 : 0)} <small className="muted">{r.unit}</small></>}</td>
            <td className="num nowrap">{tok ? '—' : r.free > 0 ? num(r.free) + ' / ' + r.per : '—'}</td>
            <td className="num nowrap">{tok ? 'per model' : r.price > 0 ? '$' + trim(r.price) + ' / ' + (UNIT1[r.unit] || r.unit) : 'Free'}</td>
            <td className="num nowrap"><b>{lc(r.bill)}</b></td>
          </tr>
        })}</tbody></table></div>
    </Card>

    <PriceEditor d={d} rows={rows} />
  </>
}

function Chart({ daily }: { daily: Data['daily'] }) {
  const mx = Math.max(0.01, ...daily.map(x => x.cost))
  const mn = Math.max(1, ...daily.map(x => x.calls))
  const H = 170
  return <div className="card mt">
    <div className="card-h"><h3><Hi n="activity" c="violet" />Daily cost &amp; calls – last 30 days</h3>
      <div className="row sm muted"><span className="row nw" style={{ gap: 6 }}><i style={{ width: 10, height: 10, borderRadius: 3, background: '#7C5CF0', display: 'inline-block' }} />Cost (USD)</span><span className="row nw" style={{ gap: 6 }}><i style={{ width: 10, height: 10, borderRadius: 10, background: '#F97316', display: 'inline-block' }} />Calls</span></div></div>
    <div style={{ position: 'relative', height: H + 24, paddingLeft: 44 }} role="img" aria-label="Daily API usage, last 30 days">
      {[0, .5, 1].map(g => <div key={g} style={{ position: 'absolute', left: 44, right: 0, bottom: 24 + g * H, borderTop: '1px solid #EEF0F5' }}><span style={{ position: 'absolute', left: -44, top: -8, fontSize: 10, color: '#98A0B3' }}>{usd(mx * g)}</span></div>)}
      <div style={{ position: 'absolute', left: 44, right: 0, top: 0, height: H, display: 'flex', alignItems: 'flex-end', gap: 4 }}>
        {daily.map(x => <div key={x.day} title={shortDay(x.day) + ': ' + usd(x.cost) + ' · ' + x.calls + ' calls'} style={{ flex: 1, height: '100%', position: 'relative', display: 'flex', alignItems: 'flex-end' }}>
          <div style={{ width: '100%', height: x.cost > 0 ? Math.max(2, x.cost / mx * (H - 14)) : 0, borderRadius: 4, background: 'linear-gradient(#8B5CF6,#6366F1)' }} />
          {x.calls > 0 && <span style={{ position: 'absolute', left: '50%', bottom: x.calls / mn * (H - 14), width: 6, height: 6, marginLeft: -3, marginBottom: -3, borderRadius: 6, background: '#F97316', opacity: .85 }} />}
        </div>)}
      </div>
      <div style={{ position: 'absolute', left: 44, right: 0, bottom: 0, height: 20, display: 'flex', gap: 4 }}>
        {daily.map((x, i) => <span key={x.day} style={{ flex: 1, textAlign: 'center', fontSize: 10.5, color: '#98A0B3', whiteSpace: 'nowrap', overflow: 'visible' }}>{i % 5 === 0 || i === 29 ? shortDay(x.day) : ''}</span>)}
      </div>
    </div>
  </div>
}

type PRow = Row & { g: Group }

function PriceEditor({ d, rows }: { d: Data; rows: PRow[] }) {
  const qc = useQueryClient()
  const [svc, setSvc] = useState<Record<string, { price: string; free: string }>>({})
  const [models, setModels] = useState<Record<string, { in: string; out: string }>>({})
  const [nm, setNm] = useState({ id: '', in: '', out: '' })
  const [fxMode, setFxMode] = useState(d.fx.mode === 'manual' ? 'manual' : 'auto')
  const [rate, setRate] = useState(d.fx.usd_rate)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    setSvc(Object.fromEntries(rows.filter(r => r.unit !== 'tokens').map(r => [r.key, { price: trim(r.price), free: String(r.free) }])))
    setModels(Object.fromEntries(d.models.map(m => [m.id, { in: String(m.in), out: String(m.out) }])))
  }, [d])
  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body: any = {
        svc: Object.fromEntries(Object.entries(svc).map(([k, x]) => [k, { price: +x.price || 0, free: +x.free || 0 }])),
        models: Object.fromEntries(Object.entries(models).filter(([, x]) => x.in !== '' || x.out !== '').map(([k, x]) => [k, { in: +x.in || 0, out: +x.out || 0 }])),
        fx_mode: fxMode,
      }
      if (nm.id.trim()) body.new_model = { id: nm.id.trim(), in: +nm.in || 0, out: +nm.out || 0 }
      if (rate !== '') body.usd_rate = +rate
      const r = await api.put('/usage/prices', body)
      toast(r.message || 'Saved.')
      setNm({ id: '', in: '', out: '' })
      qc.invalidateQueries({ queryKey: ['usage'] })
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const w = { width: 130, margin: 0 }
  return <form onSubmit={save}><details className="opt">
    <summary><Hi n="edit" c="orange" />Edit free limits, prices &amp; currency</summary>
    <div className="tw mt"><table className="tbl"><thead><tr><th>API</th><th>Free units</th><th>USD per unit</th></tr></thead>
      <tbody>{rows.filter(r => r.unit !== 'tokens').map(r => <tr key={r.key}>
        <td><b>{r.name}</b><br /><small className="muted">per {UNIT1[r.unit] || r.unit} · free limit resets every {r.per}</small></td>
        <td><input type="number" min={0} step={1} style={w} value={svc[r.key]?.free ?? ''} onChange={e => setSvc(s => ({ ...s, [r.key]: { ...s[r.key], free: e.target.value } }))} /></td>
        <td><input type="number" min={0} step={0.0001} style={w} value={svc[r.key]?.price ?? ''} onChange={e => setSvc(s => ({ ...s, [r.key]: { ...s[r.key], price: e.target.value } }))} /></td>
      </tr>)}</tbody></table></div>
    <h4 id="models">AI model prices (USD per 1 million tokens)</h4>
    <div className="tw"><table className="tbl"><thead><tr><th>Model</th><th>Input</th><th>Output</th></tr></thead>
      <tbody>{d.models.map(m => <tr key={m.id}>
        <td><code>{m.id}</code>{m.in_use && <> <Badge c="green">in use</Badge></>}</td>
        <td><input type="number" min={0} step={0.001} style={{ width: 110, margin: 0 }} value={models[m.id]?.in ?? ''} onChange={e => setModels(s => ({ ...s, [m.id]: { ...s[m.id], in: e.target.value } }))} /></td>
        <td><input type="number" min={0} step={0.001} style={{ width: 110, margin: 0 }} value={models[m.id]?.out ?? ''} onChange={e => setModels(s => ({ ...s, [m.id]: { ...s[m.id], out: e.target.value } }))} /></td>
      </tr>)}
        <tr><td><input placeholder="Add model id" style={{ margin: 0 }} value={nm.id} onChange={e => setNm({ ...nm, id: e.target.value })} /></td>
          <td><input type="number" min={0} step={0.001} style={{ width: 110, margin: 0 }} value={nm.in} onChange={e => setNm({ ...nm, in: e.target.value })} /></td>
          <td><input type="number" min={0} step={0.001} style={{ width: 110, margin: 0 }} value={nm.out} onChange={e => setNm({ ...nm, out: e.target.value })} /></td></tr>
      </tbody></table></div>
    {d.fx.currency !== 'USD' && <><h4>Currency</h4><div className="fg">
      <Field label="Rate source"><select value={fxMode} onChange={e => setFxMode(e.target.value)}>
        <option value="auto">Automatic – free exchange-rate API, refreshed daily (now 1 USD = {d.fx.symbol}{fmt(d.fx.rate)})</option>
        <option value="manual">Manual – I enter the rate</option>
      </select></Field>
      {fxMode === 'manual' && <Field label={'1 USD in ' + d.fx.currency}><input type="number" step={0.0001} value={rate} onChange={e => setRate(e.target.value)} /></Field>}
    </div></>}
    <div className="card-f" style={{ margin: '16px -16px -14px' }}><Btn v="p" ic="check" type="submit" busy={busy}>Save</Btn><span className="muted sm">Estimates use list prices and your free limits; your provider's invoice is final.</span></div>
  </details></form>
}

/** Settings → System → API usage & costs. */
export function UsageTab() {
  return <>
    <p className="sm muted mt0">Every API Setu can use – status, free limits, usage and estimated cost. <Link className="lnk" to="/usage">Open full page</Link></p>
    <UsagePage embedded />
  </>
}


