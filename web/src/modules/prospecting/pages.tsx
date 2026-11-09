// Map Scraper (searches), campaign detail and Scraped Data – v1 scraper.php, campaign.php, prospects.php.
import { FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, download, qs } from '@/lib/api'
import { ago, num } from '@/lib/format'
import { Alert, Btn, Icon, Kpi, Pager, Spinner, confirmDo, toast, usePageTitle } from '@/components/ui'
import { BulkBar, CampBadge, InitModal, LeadsTable, ListFilter, Page, ProcessModal, Row, STAGES, runBulk, useSel } from './common'

interface Campaign { id: number; keyword: string; location: string; limit_n: number; source: string; status: string; found: number; skipped: number; error: string; created_at: string; leads: number; web: number; rr: number; ct: number; rp: number; il: number }

// ------------------------------------------------------------------ Map Scraper
export function Scraper() {
  usePageTitle('Map Scraper', 'Find businesses on Google Maps – results go to Scraped Data, not your Leads')
  const [sp] = useSearchParams()
  const go = useNavigate(), qc = useQueryClient()
  const q = useQuery({ queryKey: ['campaigns'], queryFn: () => api.get<{ campaigns: Campaign[]; source: string; key_ok: boolean }>('/campaigns') })
  const [f, setF] = useState({ keyword: '', location: '', limit: '20', source: '' })
  const [busy, setBusy] = useState(false)
  const live = q.data?.campaigns.some(c => c.status === 'searching')
  useEffect(() => {
    if (!live) return
    const t = setInterval(() => qc.invalidateQueries({ queryKey: ['campaigns'] }), 4000)
    return () => clearInterval(t)
  }, [live, qc])
  if (!q.data) return <Spinner />
  const { campaigns: rows, key_ok } = q.data
  const src = f.source || q.data.source
  const t = rows.reduce((a, x) => ({ f: a.f + x.leads, w: a.w + x.web, r: a.r + x.rr, c: a.c + x.ct, p: a.p + x.rp }), { f: 0, w: 0, r: 0, c: 0, p: 0 })
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!f.keyword.trim()) { toast('Please enter a keyword.'); return }
    setBusy(true)
    try {
      const r = await api.post<{ id: number; notices: { t: string; msg: string }[] }>('/campaigns', { keyword: f.keyword, location: f.location, limit: Number(f.limit) || 20, source: src })
      r.notices?.forEach(n => toast(n.msg))
      qc.invalidateQueries({ queryKey: ['campaigns'] })
      go('/campaigns/' + r.id)
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const del = async (c: Campaign) => {
    if (!confirmDo('Delete this search and its scraped businesses? (Businesses already in Leads are kept.)')) return
    try { const r = await api.del<{ message: string }>('/campaigns/' + c.id); toast(r.message); qc.invalidateQueries({ queryKey: ['campaigns'] }) } catch (e: any) { toast(e.message) }
  }
  return <>
    <div className="card hero"><div className="hero-t"><div><span className="eyebrow"><Icon n="map" />Google Maps prospecting</span><h2>Find businesses to help</h2><p className="muted">Search any category and area. We collect website, phone, rating and reviews, then find missing phone numbers and emails on each website.</p></div>
      {!key_ok && <Link className="btn" to="/settings/data"><Icon n="alert" />Add an API key first</Link>}</div>
      <form className="sf" onSubmit={submit}>
        <label className="kw">What are you looking for?<input value={f.keyword} onChange={e => setF({ ...f, keyword: e.target.value })} placeholder="e.g. dentist, interior designer, CA firm" required autoFocus={sp.has('new')} /></label>
        <label>Where?<input value={f.location} onChange={e => setF({ ...f, location: e.target.value })} placeholder="e.g. Baner, Pune" /></label>
        <label>How many?<input type="number" min={1} max={500} value={f.limit} onChange={e => setF({ ...f, limit: e.target.value })} list="lim" required /><datalist id="lim">{[10, 20, 30, 50, 100, 200].map(n => <option key={n} value={n} />)}</datalist></label>
        <label>Data source<select value={src} onChange={e => setF({ ...f, source: e.target.value })}><option value="places">Google Places (≤60)</option><option value="serpapi">SerpAPI (100+)</option></select></label>
        <button className="btn btn-lg sbtn" disabled={busy}>{busy ? <span className="spin" /> : <Icon n="search" />}Search</button>
      </form></div>
    <div className="stats">
      <Kpi label="Searches" value={rows.length} ic="search" c="orange" foot="all time" />
      <Kpi label="Businesses found" value={num(t.f)} ic="pin" c="sky" foot={t.f ? Math.round(t.w / t.f * 100) + '% have a website' : '—'} to="/prospects" />
      <Kpi label="Reports ready" value={t.r} ic="file" c="violet" foot="AI audit PDFs" to="/reports" />
      <Kpi label="Contacted" value={t.c} ic="send" c="amber" foot={t.c ? (Math.round(t.p / t.c * 1000) / 10) + '% replied' : 'no outreach yet'} />
    </div>
    <div className="card p0"><div className="card-h"><h3><span className="hi c-orange"><Icon n="list" /></span>Your searches</h3><span className="bdg c-gray">{rows.length}</span></div>
      {!rows.length ? <div className="empty"><Icon n="map" c="eic" /><h4>No searches yet</h4><p>Type a category and an area above – results appear here and in Scraped Data within seconds.</p></div> :
        <div className="tw"><table className="tbl"><thead><tr><th className="sn">#</th><th>Search</th><th>Progress</th><th className="num">Website</th><th className="num">Reports</th><th className="num">Contacted</th><th className="num">Replied</th><th className="num">In Leads</th><th>Status</th><th></th></tr></thead>
          <tbody>{rows.map((c, i) => { const pc = Math.min(100, Math.round(c.leads / Math.max(1, c.limit_n) * 100)); return <tr key={c.id}>
            <td className="sn">{i + 1}</td>
            <td className="biz"><Link to={'/campaigns/' + c.id}><b>{c.keyword}</b></Link><small><Icon n="pin" c="xs" /> {c.location || 'Any location'} · {c.source.toUpperCase()} · {ago(c.created_at)}</small></td>
            <td style={{ minWidth: 150 }}><div className={'pb ' + (c.status === 'error' ? 'r' : 'g')}><i style={{ width: pc + '%' }} /></div><small className="pbm">{c.leads} of {c.limit_n} listings{c.skipped ? ' · ' + c.skipped + ' already had' : ''}</small></td>
            <td className="num">{c.web}</td><td className="num">{c.rr}</td><td className="num">{c.ct}</td><td className="num">{c.rp}</td><td className="num">{c.il}</td>
            <td><CampBadge s={c.status} /></td>
            <td className="act"><Link className="btn btn-sm" to={'/campaigns/' + c.id}>Open</Link><button className="ib red" title="Delete" onClick={() => del(c)}><Icon n="trash" /></button></td>
          </tr> })}</tbody></table></div>}
    </div>
  </>
}

// ------------------------------------------------------------------ Campaign detail
interface Counts { t: number; nw: number; nn: number; rr: number; ct: number; rp: number; fl: number; wk: number }

export function CampaignPage() {
  const id = Number(useParams().id)
  const [sp, setSp] = useSearchParams()
  const qc = useQueryClient()
  const f = sp.get('f') || '', p = Number(sp.get('p') || 1)
  const head = useQuery({ queryKey: ['campaign', id], queryFn: () => api.get<{ campaign: Campaign; counts: Counts }>('/campaigns/' + id) })
  const list = useQuery({ queryKey: ['campaign', id, 'leads', f, p], queryFn: () => api.get<Page>('/campaigns/' + id + '/leads', { f, p }), placeholderData: x => x })
  const c = head.data?.campaign, cnt = head.data?.counts
  usePageTitle(c ? c.keyword + (c.location ? ' · ' + c.location : '') : 'Search', 'Map search results')
  const rows: Row[] = list.data?.rows || []
  const sel = useSel(rows)
  const [more, setMore] = useState('20')
  const [init, setInit] = useState(false)
  const [initNew, setInitNew] = useState(false)
  const [proc, setProc] = useState(0)
  const live = c?.status === 'searching' || (cnt?.wk || 0) > 0
  const refresh = () => qc.invalidateQueries({ queryKey: ['campaign', id] })
  useEffect(() => {
    if (!live) return
    const t = setInterval(refresh, 3000)
    return () => clearInterval(t)
  }, [live, id])
  useEffect(() => { sel.clear() }, [f, p])
  if (head.isError) return <Alert t="err">Search not found.</Alert>
  if (!c || !cnt) return <Spinner />
  const set = (k: string, v: string) => { const n = new URLSearchParams(sp); v ? n.set(k, v) : n.delete(k); if (k !== 'p') n.delete('p'); setSp(n) }
  const post = async (url: string, body?: unknown) => { try { const r = await api.post<{ message?: string }>(url, body); r.message && toast(r.message); refresh(); qc.invalidateQueries({ queryKey: ['campaigns'] }) } catch (e: any) { toast(e.message) } }
  const abort = (ids: number[]) => { if (confirmDo('Stop processing the selected businesses?')) post('/prospects/abort', { ids }) }
  const tabs: [string, string, number][] = [['', 'All', cnt.t], ['new', 'New', cnt.nn], ['web', 'With website', cnt.t - cnt.nw], ['noweb', 'No website', cnt.nw], ['ready', 'Report ready', cnt.rr], ['contacted', 'Contacted', cnt.ct], ['replied', 'Replied', cnt.rp], ['failed', 'Failed', cnt.fl], ['working', 'Processing', cnt.wk]]
  const pc = Math.min(100, Math.round(c.found / Math.max(1, c.limit_n) * 100))
  const filter: ListFilter = { campaign_id: id, f }
  return <>
    <Link className="back" to="/scraper">← All searches</Link>
    <div className="card mt"><div className="lh"><span className="avc c-orange"><Icon n="map" /></span><div className="grow"><h2>{c.keyword}</h2>
      <div className="meta"><span><Icon n="pin" />{c.location || 'Any location'}</span><span><Icon n="db" />{c.source.toUpperCase()}</span><span><Icon n="clock" />created {ago(c.created_at)}</span><CampBadge s={c.status} /></div></div>
      <div className="pha">
        {c.status === 'searching' ? <Btn v="r" ic="stop" onClick={() => post('/campaigns/' + id + '/stop')}>Stop search</Btn> :
          <form className="inl" onSubmit={e => { e.preventDefault(); post('/campaigns/' + id + '/more', { n: Number(more) || 20 }) }}><input type="number" value={more} min={1} max={200} className="sm-in" title="How many more" onChange={e => setMore(e.target.value)} /><Btn type="submit" ic="plus">Fetch more</Btn></form>}
        <Btn ic="download" onClick={() => download('/campaigns/' + id + '/export' + qs({ f }))}>CSV</Btn>
        {cnt.nn > 0 && <Btn v="p" ic="zap" onClick={() => setInitNew(true)}>Process all new ({cnt.nn})</Btn>}
      </div></div>
      <div className="row mt"><div className="grow"><div className="pb g"><i style={{ width: pc + '%' }} /></div></div><small className="muted nowrap">{c.found} of {c.limit_n} listings</small></div>
      {c.skipped > 0 && <div className="al al-info mt mb0"><Icon n="check" /><span><b>{c.skipped} business{c.skipped > 1 ? 'es were' : ' was'} already in your data</b> (earlier searches or Leads) and skipped – no duplicates and no API cost for them.{c.status === 'ready' && c.found < c.limit_n ? ' Google has no more new results for this search – try a nearby area or another keyword.' : ''}</span></div>}
      {c.error && <div className="al al-err mt mb0"><Icon n="alert" /><span>{c.error}</span></div>}
    </div>
    <div className="stats">
      <Kpi label="Found" value={cnt.t} ic="pin" c="sky" foot={(cnt.t - cnt.nw) + ' with website'} />
      <Kpi label="Reports ready" value={cnt.rr} ic="file" c="violet" foot="ready to send" />
      <Kpi label="Contacted" value={cnt.ct} ic="send" c="amber" foot={cnt.rp + ' replied'} />
      <Kpi label="Processing" value={cnt.wk} ic="zap" c="orange" foot={cnt.fl + ' failed'} />
    </div>
    <div className="tabs">{tabs.map(([k, l, n]) => k === 'working' && !n && f !== 'working' ? null : <a key={k} href="#" className={f === k ? 'on' : ''} onClick={e => { e.preventDefault(); set('f', k) }}>{l}<em>{n}</em></a>)}</div>
    <div className="card p0">
      <BulkBar sel={sel} total={list.data?.total || 0} filter={filter} onDone={refresh} initOpen={init} setInitOpen={setInit} />
      {!list.data ? <Spinner /> : <LeadsTable rows={rows} sel={sel} offset={(list.data.page - 1) * list.data.per} emptyMsg={c.status === 'searching' ? 'Searching Google Maps… listings will appear here in a few seconds.' : 'Nothing matches this filter.'} onAbort={abort} onProc={setProc} />}
      {list.data && list.data.pages > 1 && <Pager page={list.data.page} pages={list.data.pages} total={list.data.total} label={'businesses · page ' + list.data.page + ' of ' + list.data.pages} onPage={n => set('p', String(n))} />}
    </div>
    {proc > 0 && <ProcessModal id={proc} onClose={() => setProc(0)} />}
    {initNew && <InitModal n={cnt.nn} onClose={() => setInitNew(false)} onStart={async o => { setInitNew(false); if (await runBulk('initiate', { all: true, filter: { campaign_id: id, f: 'new' } }, o)) refresh() }} />}
  </>
}

// ------------------------------------------------------------------ Scraped Data
interface Summary { kpi: { t: number; wk: number; rr: number; rp: number; il: number }; campaigns: { id: number; keyword: string; location: string }[]; stages: [string, string, string][] }

export function Prospects() {
  usePageTitle('Scraped Data', 'Businesses found by the Map Scraper. Move won or interested ones to Leads.')
  const [sp, setSp] = useSearchParams()
  const qc = useQueryClient()
  const fl = { q: sp.get('q') || '', stage: sp.get('stage') || '', cid: sp.get('cid') || '', web: sp.get('web') || '', il: sp.get('il') || '' }
  const p = Number(sp.get('p') || 1)
  const [q, setQ] = useState(fl.q)
  useEffect(() => setQ(fl.q), [fl.q])
  const sum = useQuery({ queryKey: ['prospects', 'summary'], queryFn: () => api.get<Summary>('/prospects/summary') })
  const list = useQuery({ queryKey: ['prospects', 'list', fl, p], queryFn: () => api.get<Page>('/prospects', { ...fl, p }), placeholderData: x => x })
  const rows = list.data?.rows || []
  const sel = useSel(rows)
  const [init, setInit] = useState(false)
  const [proc, setProc] = useState(0)
  const wk = sum.data?.kpi.wk || 0
  const refresh = () => qc.invalidateQueries({ queryKey: ['prospects'] })
  useEffect(() => {
    if (!wk) return
    const t = setInterval(refresh, 4000)
    return () => clearInterval(t)
  }, [wk])
  useEffect(() => { sel.clear() }, [sp.toString()])
  const set = (k: string, v: string) => { const n = new URLSearchParams(sp); v ? n.set(k, v) : n.delete(k); if (k !== 'p') n.delete('p'); setSp(n) }
  const abort = async (ids: number[]) => {
    if (!confirmDo('Stop processing the selected businesses?')) return
    try { const r = await api.post<{ message: string }>('/prospects/abort', { ids }); toast(r.message); refresh() } catch (e: any) { toast(e.message) }
  }
  const nf = Object.values(fl).filter(Boolean).length
  const k = sum.data?.kpi
  const filter: ListFilter = { q: fl.q, stage: fl.stage, cid: Number(fl.cid) || 0, web: fl.web, il: fl.il }
  const trim = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1) + '…' : s)
  return <>
    <div className="stats">
      <Kpi label="Scraped businesses" value={num(k?.t)} ic="db" c="sky" foot={'from ' + (sum.data?.campaigns.length || 0) + ' searches'} />
      <Kpi label="Processing now" value={k?.wk ?? '—'} ic="zap" c="orange" foot="audits & reports" to="/prospects?stage=working" />
      <Kpi label="Reports ready" value={k?.rr ?? '—'} ic="file" c="violet" foot="waiting to be sent" to="/prospects?stage=report_ready" />
      <Kpi label="Moved to Leads" value={k?.il ?? '—'} ic="users" c="green" foot={(k?.rp || 0) + ' replied'} to="/prospects?il=1" />
    </div>
    <form className="fbar" onSubmit={e => { e.preventDefault(); set('q', q.trim()) }}>
      <label className="srch"><Icon n="search" /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search name, phone, email, category…" /></label>
      <select className={fl.cid ? 'on' : ''} value={fl.cid} onChange={e => set('cid', e.target.value)}><option value="">All searches</option>{sum.data?.campaigns.map(c => <option key={c.id} value={c.id}>{trim(c.keyword + (c.location ? ' · ' + c.location : ''), 40)}</option>)}</select>
      <select className={fl.stage ? 'on' : ''} value={fl.stage} onChange={e => set('stage', e.target.value)}><option value="">Any status</option><option value="working">Processing now</option>{Object.entries(STAGES).map(([s, [lb]]) => <option key={s} value={s}>{lb}</option>)}<option value="replied_any">Has replied</option></select>
      <select className={fl.web ? 'on' : ''} value={fl.web} onChange={e => set('web', e.target.value)}><option value="">Website: any</option><option value="1">Has website</option><option value="0">No website</option></select>
      <select className={fl.il ? 'on' : ''} value={fl.il} onChange={e => set('il', e.target.value)}><option value="">Leads: any</option><option value="0">Not in Leads</option><option value="1">In Leads</option></select>
      {nf > 0 && <Link className="clr-f" to="/prospects">Clear ({nf})</Link>}
      <span className="sep" />
      <Btn v="n" type="submit" ic="filter">Apply</Btn>
      <Btn ic="download" title="Export CSV" onClick={() => download('/prospects/export' + qs(fl))}>CSV</Btn>
    </form>
    <div className="card p0">
      <BulkBar sel={sel} total={list.data?.total || 0} filter={filter} onDone={refresh} initOpen={init} setInitOpen={setInit} />
      {!list.data ? <Spinner /> : <LeadsTable rows={rows} sel={sel} offset={(list.data.page - 1) * list.data.per} emptyMsg="No scraped businesses match. Run a search in Map Scraper." onAbort={abort} onProc={setProc} />}
      {list.data && list.data.pages > 1 && <Pager page={list.data.page} pages={list.data.pages} total={list.data.total} onPage={n => set('p', String(n))} />}
    </div>
    {proc > 0 && <ProcessModal id={proc} onClose={() => setProc(0)} />}
  </>
}
