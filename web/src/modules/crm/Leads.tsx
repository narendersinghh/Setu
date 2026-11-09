// Leads list + pipeline board (v1 leads.php), filters, bulk actions, export.
import { DragEvent, FormEvent, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, download, qs } from '@/lib/api'
import { useCan, useMe, useMod } from '@/lib/session'
import { ago, money, num } from '@/lib/format'
import { Btn, Icon, Kpi, Modal, Pager, Spinner, act, confirmDo, toast, usePageTitle, useSelection } from '@/components/ui'
import { CC, LeadRow, SOURCES, STATUSES, SrcBadge, StatusBadge, avColor, fmtShort, isLate, tagList, telHref, useMeta, waLink } from './common'

interface ListRes {
  rows: LeadRow[]; total: number; page: number; pages: number; per: number
  kpi: { total: number; new: number; today: number; won: number; won_value: number; pipeline: number; due: number; unassigned: number }
  status: Record<string, number>; sources: Record<string, number>
}
interface BoardRes { columns: Record<string, LeadRow[]>; counts: Record<string, number>; limit: number }

const FKEYS = ['q', 'status', 'src', 'tag', 'own', 'fu', 'sort', 'view', 'p'] as const

export default function Leads() {
  usePageTitle('Leads', 'Your CRM – enquiries from forms, ads, bookings and won prospects')
  const me = useMe(), can = useCan(), mod = useMod()
  const adm = can('settings')
  const meta = useMeta().data
  const sym = me.settings.currency_symbol || '₹'
  const [sp, setSp] = useSearchParams()
  const f = Object.fromEntries(FKEYS.map(k => [k, sp.get(k) || ''])) as Record<(typeof FKEYS)[number], string>
  const board = f.view === 'board'
  const set = (x: Partial<Record<(typeof FKEYS)[number], string>>) => {
    const n = { ...f, p: '', ...x }
    const o: Record<string, string> = {}
    for (const k of FKEYS) if (n[k]) o[k] = n[k]
    setSp(o)
  }
  const [q, setQ] = useState(f.q)
  const [tag, setTag] = useState(f.tag)
  useEffect(() => { setQ(f.q); setTag(f.tag) }, [f.q, f.tag])
  const params = { q: f.q, status: f.status, src: f.src, tag: f.tag, own: f.own, fu: f.fu, sort: f.sort, p: f.p }
  const qc = useQueryClient()
  const list = useQuery({ queryKey: ['leads', params], queryFn: () => api.get<ListRes>('/leads', params), placeholderData: keepPreviousData })
  const bq = useQuery({ queryKey: ['leads-board', { ...params, status: '', p: '' }], queryFn: () => api.get<BoardRes>('/leads', { ...params, status: '', p: '', view: 'board' }), enabled: board })
  const sel = useSelection<number>()
  const autos = useQuery({ queryKey: ['automations-pick'], queryFn: () => api.get<any>('/automations').then(r => (Array.isArray(r) ? r : r.rows || r.automations || []) as { id: number; name: string }[]).catch(() => []), enabled: can('automations') })
  const [dd, setDd] = useState('')
  const [tagOpen, setTagOpen] = useState(false)
  const [bulkTag, setBulkTag] = useState('')
  const refresh = () => { qc.invalidateQueries({ queryKey: ['leads'] }); qc.invalidateQueries({ queryKey: ['leads-board'] }); qc.invalidateQueries({ queryKey: ['nav'] }) }
  useEffect(() => { sel.clear() }, [sp.toString()])

  const d = list.data
  const k = d?.kpi
  const nf = [f.q, board ? '' : f.status, f.tag, f.own, f.fu].filter(Boolean).length
  const users = meta?.users || []

  const bulk = async (action: string, value = '', confirm?: string) => {
    setDd('')
    if (!sel.ids.length) return toast('Select at least one lead.')
    if (confirm && !confirmDo(confirm)) return
    const r = await act(() => api.post<{ message: string }>('/leads/bulk', { action, ids: sel.ids, value }))
    if (r) { toast(r.message); sel.clear(); refresh() }
  }
  const startAuto = async (id: number, name: string) => {
    setDd('')
    if (!sel.ids.length) return toast('Select at least one lead.')
    if (!confirmDo('Start this automation for the selected leads?')) return
    let n = 0
    for (const lid of sel.ids) { try { await api.post('/leads/' + lid + '/automations', { automation_id: id }); n++ } catch { /* lead does not match the conditions */ } }
    toast(n + ' lead(s) added to automation "' + name + '".'); sel.clear(); refresh()
  }
  const initiate = async () => {
    if (!sel.ids.length) return toast('Select at least one lead.')
    if (!confirmDo('Run the website & Google audit + PDF report for the selected leads?')) return
    const r = await act(() => api.post<any>('/prospects/process', { ids: sel.ids, steps: ['audit', 'report'] }))
    if (r) { toast(r.message || sel.ids.length + ' lead(s) queued for audit & report.'); sel.clear(); refresh() }
  }
  const search = (e: FormEvent) => { e.preventDefault(); set({ q }) }
  const exportCsv = () => download('/leads/export' + qs({ q: f.q, status: f.status, src: f.src, tag: f.tag, own: f.own, fu: f.fu }))

  const rows = d?.rows || []
  const allOn = rows.length > 0 && rows.every(r => sel.has(r.id))
  return <>
    {k && <div className="stats">
      <Kpi label={me.user.role === 'sales' ? 'My leads' : 'Total leads'} value={num(k.total)} ic="users" c="indigo" foot={k.today + ' added today'} />
      <Kpi label="New / uncontacted" value={k.new} ic="zap" c="sky" foot="need a first touch" to={'/leads' + qs({ ...cleanF(f), status: 'new', p: '' })} />
      <Kpi label="Follow-ups due" value={k.due} ic="clock" c="orange" foot={k.due ? <span className="dn">overdue or today</span> : 'all caught up'} to={'/leads' + qs({ ...cleanF(f), fu: 'due', p: '' })} />
      <Kpi label="Open pipeline" value={money(k.pipeline, sym)} ic="kanban" c="violet" foot={adm ? k.unassigned + ' unassigned' : 'deal value in progress'} to={'/leads' + qs({ ...cleanF(f), status: '', p: '' })} />
      <Kpi label="Won" value={money(k.won_value, sym)} ic="target" c="green" foot={k.won + ' deals'} to={'/leads' + qs({ ...cleanF(f), status: 'won', p: '' })} />
    </div>}
    <div className="ph"><div><b style={{ fontSize: 15, color: 'var(--ink)' }}>{num(board ? Object.values(bq.data?.counts || {}).reduce((a, b) => a + b, 0) : d?.total)} lead{(d?.total ?? 0) === 1 ? '' : 's'}</b><span className="muted sm">{nf || f.src ? ' match your filters' : ' in total'}</span></div>
      <div className="pha">
        <div className="tabs" style={{ margin: 0 }}><a href="#" className={board ? '' : 'on'} onClick={e => { e.preventDefault(); set({ view: '' }) }}><Icon n="list" />List</a><a href="#" className={board ? 'on' : ''} onClick={e => { e.preventDefault(); set({ view: 'board' }) }}><Icon n="kanban" />Board</a></div>
        {adm && <><Link className="btn" to="/leads/new?tab=import"><Icon n="upload" />Import</Link><Btn ic="download" onClick={exportCsv}>Export</Btn></>}
        <Link className="btn btn-p" to="/leads/new"><Icon n="plus" />Add lead</Link>
      </div></div>
    <form className="flt card" onSubmit={search}>
      <div className="flt-r"><label className="srch"><Icon n="search" /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search name, phone, email, city, tag or any field… (press Enter)" /></label>
        {(nf > 0 || !!f.src) && <a className="btn btn-ghost" href="#" onClick={e => { e.preventDefault(); setSp(board ? { view: 'board' } : {}) }}><Icon n="x" />Clear all filters</a>}</div>
      {!board && <div className="flt-g"><span className="flt-l"><Icon n="target" />Status</span><div className="chips mb0">
        <a href="#" onClick={e => { e.preventDefault(); set({ status: '' }) }} className={'all c-indigo ' + (f.status === '' ? 'on' : '')}>All <em>{Object.values(d?.status || {}).reduce((a, b) => a + b, 0)}</em></a>
        {STATUSES.map(s => <a key={s.key} href="#" onClick={e => { e.preventDefault(); set({ status: s.key }) }} className={'all c-' + CC[s.color] + ' ' + (f.status === s.key ? 'on' : '')}><i className="dot" />{s.label} <em>{d?.status?.[s.key] || 0}</em></a>)}
      </div></div>}
      <div className="flt-g"><span className="flt-l"><Icon n="plug" />Source</span><div className="chips mb0">
        <a href="#" onClick={e => { e.preventDefault(); set({ src: '' }) }} className={'all c-indigo ' + (f.src === '' ? 'on' : '')}>All <em>{Object.values(d?.sources || {}).reduce((a, b) => a + b, 0)}</em></a>
        {Object.entries(SOURCES).map(([key, [lb, c, ic]]) => (!d?.sources?.[key] && f.src !== key) ? null : <a key={key} href="#" onClick={e => { e.preventDefault(); set({ src: key }) }} className={'src-' + c + ' ' + (f.src === key ? 'on' : '')}><Icon n={ic} />{lb} <em>{d?.sources?.[key] || 0}</em></a>)}
      </div></div>
      <div className="flt-g"><span className="flt-l"><Icon n="sliders" />More</span><div className="flt-m">
        <label className="c-blue">Owner<select value={f.own} onChange={e => set({ own: e.target.value })}><option value="">Anyone</option><option value="me">Assigned to me</option><option value="none">Unassigned</option>{users.filter(u => u.id !== me.user.id).map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select></label>
        <label className="c-orange">Follow-up<select value={f.fu} onChange={e => set({ fu: e.target.value })}><option value="">Any time</option><option value="due">Due or overdue</option></select></label>
        <label className="c-violet">Tag<input value={tag} placeholder="e.g. hot" onChange={e => setTag(e.target.value)} onBlur={() => tag !== f.tag && set({ tag })} /></label>
        {!board && <label className="c-teal">Sort<select value={f.sort} onChange={e => set({ sort: e.target.value })}><option value="">Newest first</option><option value="old">Oldest first</option><option value="activity">Recent activity</option><option value="follow">Follow-up date</option><option value="deal">Deal value</option><option value="name">Name A–Z</option></select></label>}
      </div></div>
    </form>
    {board ? <Board data={bq.data} loading={bq.isLoading} sym={sym} onMoved={refresh} /> :
      <div className="card p0">
        <div className={'bulk' + (sel.ids.length ? '' : ' idle')}>
          <span className="bsel"><b>{sel.ids.length}</b><span className="bs-t"> selected</span></span>
          <div className={'dd l' + (dd === 'st' ? ' open' : '')}><Btn sm ic="target" onClick={() => setDd(dd === 'st' ? '' : 'st')}>Set status ▾</Btn><div className="ddm">{STATUSES.map(s => <button key={s.key} type="button" onClick={() => bulk('status', s.key)}>{s.label}</button>)}</div></div>
          {adm && users.length > 0 && <div className={'dd l' + (dd === 'own' ? ' open' : '')}><Btn sm ic="user" onClick={() => setDd(dd === 'own' ? '' : 'own')}>Assign ▾</Btn><div className="ddm">{users.map(u => <button key={u.id} type="button" onClick={() => bulk('owner', String(u.id))}>{u.name}</button>)}<hr /><button type="button" onClick={() => bulk('owner', '0')}>Unassign</button></div></div>}
          <Btn sm ic="tag" onClick={() => setTagOpen(true)}>Add tag</Btn>
          {(autos.data?.length ?? 0) > 0 && <div className={'dd l' + (dd === 'au' ? ' open' : '')}><Btn sm ic="bot" onClick={() => setDd(dd === 'au' ? '' : 'au')}>Automation ▾</Btn><div className="ddm">{autos.data!.map(a => <button key={a.id} type="button" onClick={() => startAuto(a.id, a.name)}>{a.name}</button>)}</div></div>}
          {can('prospecting') && mod('prospecting') && <Btn sm ic="file" onClick={initiate}>Audit &amp; report</Btn>}
          {adm && <Btn sm v="r" ic="trash" title="Delete" onClick={() => bulk('delete', '', 'Delete the selected leads and all their history?')} />}
        </div>
        {!d ? <Spinner /> : !rows.length ? <div className="empty"><Icon n="users" c="eic" /><h4>{d.total || nf || f.src ? 'No leads match' : 'No leads yet'}</h4><p>{nf || f.src ? 'Try clearing the filters.' : 'Add a lead, import a CSV, or connect website forms and ads in Settings → Lead capture.'}</p><Link className="btn btn-p" to="/leads/new"><Icon n="plus" />Add lead</Link></div> :
          <>
            <div className="tw"><table className="tbl"><thead><tr><th className="cb"><input type="checkbox" checked={allOn} onChange={e => sel.all(rows.map(r => r.id), e.target.checked)} /></th><th className="sn">#</th><th>Lead</th><th>Contact</th><th>Source</th><th>Status</th><th className="num">Deal</th><th>Follow-up</th><th>Owner</th><th></th></tr></thead>
              <tbody>{rows.map((l, i) => {
                const nm = l.contact_name || l.name
                const late = isLate(l.follow_up_at, l.status)
                const sub = [l.contact_name && l.name !== l.contact_name ? l.name : l.city, l.service].filter(Boolean).join(' · ')
                return <tr key={l.id}>
                  <td className="cb"><input type="checkbox" checked={sel.has(l.id)} onChange={() => sel.toggle(l.id)} /></td>
                  <td className="sn">{(d.page - 1) * d.per + i + 1}</td>
                  <td><div className="who"><span className={'avc c-' + avColor(nm)}>{nm.charAt(0).toUpperCase()}</span><div className="biz"><Link to={'/leads/' + l.id}><b>{nm}</b></Link><small>{sub}</small>{tagList(l.tags).slice(0, 3).map(t => <span key={t}><span className="bdg c-gray sm">{t}</span> </span>)}</div></div></td>
                  <td className="ctc">{l.phone && <span><Icon n="phone" />{l.phone}</span>}{l.email && <span><Icon n="mail" />{l.email.length > 28 ? l.email.slice(0, 27) + '…' : l.email}</span>}{!l.phone && !l.email && <span className="muted">—</span>}</td>
                  <td><SrcBadge s={l.source} />{l.source_detail && <small className="muted ell" style={{ display: 'block', maxWidth: 170 }}>{l.source_detail}</small>}</td>
                  <td><StatusBadge s={l.status} /></td>
                  <td className="num nowrap">{l.deal_value ? money(l.deal_value, sym) : <span className="muted">—</span>}</td>
                  <td className={'nowrap sm ' + (late ? 'red' : '')}>{l.follow_up_at ? <>{late && <Icon n="alert" c="xs" />} {fmtShort(l.follow_up_at)}</> : <span className="muted">—</span>}</td>
                  <td className="sm nowrap">{l.owner || <span className="muted">Unassigned</span>}<small className="muted" style={{ display: 'block' }}>{ago(l.last_activity || l.created_at)}</small></td>
                  <td className="act">{l.phone && <><a className="ib" href={telHref(l.phone)} title="Call"><Icon n="phone" /></a><a className="ib" href={waLink(l.phone, meta?.cc || '91')} target="_blank" rel="noopener noreferrer" title="WhatsApp"><Icon n="wa" /></a></>}<Link className="ib" to={'/leads/' + l.id} title="Open"><Icon n="eye" /></Link></td>
                </tr>
              })}</tbody></table></div>
            <Pager page={d.page} pages={d.pages} total={d.total} label="leads" onPage={p => set({ p: String(p) })} />
          </>}
      </div>}
    <Modal open={tagOpen} onClose={() => setTagOpen(false)} title="Add a tag" ic="tag" c="violet" footer={<><Btn onClick={() => setTagOpen(false)}>Cancel</Btn><Btn v="p" ic="check" onClick={async () => { setTagOpen(false); await bulk('tag', bulkTag); setBulkTag('') }}>Add tag</Btn></>}>
      <label>Tag<input autoFocus value={bulkTag} onChange={e => setBulkTag(e.target.value)} placeholder="e.g. hot" /><small>Tags can start automations.</small></label>
    </Modal>
  </>
}

function cleanF(f: Record<string, string>) { const o: Record<string, string> = {}; for (const k in f) if (f[k]) o[k] = f[k]; return o }

// ------------------------------------------------------------------ pipeline board (drag & drop)
function Board({ data, loading, sym, onMoved }: { data?: BoardRes; loading: boolean; sym: string; onMoved: () => void }) {
  const [cols, setCols] = useState<Record<string, LeadRow[]>>({})
  const [counts, setCounts] = useState<Record<string, number>>({})
  const [drag, setDrag] = useState<LeadRow | null>(null)
  const [over, setOver] = useState('')
  useEffect(() => { if (data) { setCols(data.columns); setCounts(data.counts) } }, [data])
  if (loading || !data) return <Spinner />
  const drop = async (e: DragEvent, to: string) => {
    e.preventDefault(); setOver('')
    const l = drag
    setDrag(null)
    if (!l || l.status === to) return
    const from = l.status
    const moved = { ...l, status: to }
    setCols(c => ({ ...c, [from]: (c[from] || []).filter(x => x.id !== l.id), [to]: [moved, ...(c[to] || [])] }))
    setCounts(c => ({ ...c, [from]: Math.max(0, (c[from] || 0) - 1), [to]: (c[to] || 0) + 1 }))
    try { await api.post('/leads/' + l.id + '/status', { status: to }); toast('Status updated'); onMoved() } catch (err: any) { toast(err.message); onMoved() }
  }
  return <div className="kb">{STATUSES.map(s => {
    const list = cols[s.key] || []
    const n = counts[s.key] || 0
    return <div key={s.key} className={'kc' + (over === s.key ? ' over' : '')}
      onDragOver={e => { e.preventDefault(); setOver(s.key) }} onDragLeave={() => setOver('')} onDrop={e => drop(e, s.key)}>
      <div className="kc-h"><span className={'c-' + CC[s.color]}><i />{s.label}</span><em>{n}</em></div>
      {list.map(l => {
        const nm = l.contact_name || l.name
        return <div key={l.id} className={'kcard c-' + CC[s.color] + (drag?.id === l.id ? ' drag' : '')} draggable onDragStart={() => setDrag(l)} onDragEnd={() => setDrag(null)}>
          <Link to={'/leads/' + l.id}><b>{nm}</b></Link>
          {l.contact_name && l.name !== l.contact_name && <small><Icon n="grid" />{l.name}</small>}
          {l.phone && <small><Icon n="phone" />{l.phone}</small>}
          {l.deal_value ? <small><Icon n="target" />{money(l.deal_value, sym)}</small> : null}
          {l.follow_up_at && <small className={isLate(l.follow_up_at, l.status) ? 'red' : ''}><Icon n="clock" />{fmtShort(l.follow_up_at)}</small>}
          <small><SrcBadge s={l.source} />{l.owner && <> · {l.owner}</>}</small>
        </div>
      })}
      {n > list.length && <p className="hint" style={{ textAlign: 'center' }}>+{n - list.length} more – use the list view</p>}
    </div>
  })}</div>
}
