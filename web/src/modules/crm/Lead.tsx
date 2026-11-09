// Lead detail (v1 lead.php): header, pipeline, overview, details & custom fields, journey, and the
// sections of other modules (conversations, meetings, AI calls, automations, process log).
import { FormEvent, ReactNode, useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useCan, useMe, useMod } from '@/lib/session'
import { ago, date, money, safeUrl, toLocalInput } from '@/lib/format'
import { Alert, Btn, Hi, Icon, Modal, Spinner, act, confirmDo, toast, usePageTitle, useInterval } from '@/components/ui'
import LeadAutomations from '@/modules/automation/LeadAutomations'
import { CField, ChBadge, CopyText, CustomInput, EVIC, SOURCES, STATUSES, SrcBadge, StageBadge, StatusBadge, avColor, localToISO, statusOf, stageLabel, telHref, useMeta, waLink } from './common'

interface LeadFull {
  id: number; name: string; contact_name: string; category: string; address: string; city: string; phone: string; phone_source: string; email: string; website: string
  rating: number | null; reviews: number | null; maps_url: string; position: number; campaign_id: number; keyword: string; stage: string; email_status: string; wa_status: string
  replied_at: string | null; replied_via: string; notes: string; last_activity: string | null; is_lead: boolean; source: string; source_detail: string; alt_phone: string
  service: string; deal_value: number | null; status: string; tags: string; follow_up_at: string | null; assigned_to: number | null; custom: Record<string, any>
  message: string; progress: number; progress_msg: string; last_error: string; created_at: string; map_score: number | null; web_score: number | null
}
interface LeadRes {
  lead: LeadFull; owner: string; fields: CField[]; counts: Record<string, number>; next_appt: { id: number; title: string; start_at: string; meet_link: string } | null
  report: { id: number; token: string; views: number; created_at: string; url: string } | null; location: string; booking_link: string; wa_phone: string; busy: boolean
}
interface Ev { id: number; type: string; channel: string; message: string; created_at: string }

/** Fetch a section owned by another module; 404 or any failure → null (empty state). */
function useSection<T>(key: unknown[], url: string, enabled = true) {
  return useQuery({ queryKey: key, queryFn: () => api.get<T>(url).catch(() => null), enabled, retry: false })
}
const asRows = (d: any): any[] => (Array.isArray(d) ? d : d?.rows || d?.items || [])

const dt = (ts: string) => new Date(ts).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', hour12: true })
const dDay = (ts: string) => new Date(ts).toLocaleString('en-GB', { weekday: 'short', day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hour12: true })

type TabK = 'overview' | 'details' | 'journey' | 'messages' | 'meetings' | 'calls' | 'automations'

export default function Lead() {
  const id = Number(useParams().id)
  const me = useMe(), can = useCan(), mod = useMod()
  const adm = can('settings'), pro = can('prospecting') && mod('prospecting')
  const sym = me.settings.currency_symbol || '₹'
  const meta = useMeta().data
  const qc = useQueryClient(), go = useNavigate()
  const [sp, setSp] = useSearchParams()
  const q = useQuery({ queryKey: ['lead', id], queryFn: () => api.get<LeadRes>('/leads/' + id), retry: false })
  const ev = useQuery({ queryKey: ['lead', id, 'journey'], queryFn: () => api.get<Ev[]>('/leads/' + id + '/journey'), enabled: !!q.data })
  const [modal, setModal] = useState('')
  const [dd, setDd] = useState(false)
  const d = q.data
  const l = d?.lead
  const nm = l ? (l.contact_name && l.is_lead ? l.contact_name : l.name) : ''
  usePageTitle(nm || 'Lead', l ? [l.contact_name && l.is_lead && l.name !== l.contact_name ? l.name : '', l.category, l.city].filter(Boolean).join(' · ') : '')
  useInterval(() => { qc.invalidateQueries({ queryKey: ['lead', id] }) }, d?.busy ? 5000 : null)
  if (q.error) return <><Link className="back" to="/leads">← Leads</Link><Alert t="err">{(q.error as Error).message}</Alert></>
  if (!d || !l) return <Spinner />
  const refresh = () => { qc.invalidateQueries({ queryKey: ['lead', id] }); qc.invalidateQueries({ queryKey: ['leads'] }); qc.invalidateQueries({ queryKey: ['nav'] }) }
  const post = async (url: string, body: unknown = {}, ok?: string) => { setDd(false); const r = await act(() => api.post<any>(url, body)); if (r) { toast(r.message || ok || 'Done.'); refresh() } return r }
  const del = async () => {
    setDd(false)
    if (!confirmDo('Delete this record and all its history?')) return
    const r = await act(() => api.del('/leads/' + id), 'Deleted.')
    if (r) { qc.invalidateQueries({ queryKey: ['leads'] }); go(l.is_lead ? '/leads' : '/prospects') }
  }
  const tabs: { k: TabK; label: string; ic: string; c: string; n?: number }[] = [
    { k: 'overview', label: 'Overview', ic: 'grid', c: 'indigo' }, { k: 'details', label: 'Details', ic: 'edit', c: 'blue' },
    { k: 'journey', label: 'Journey', ic: 'activity', c: 'violet', n: d.counts.journey }, { k: 'messages', label: 'Conversations', ic: 'msg', c: 'green', n: d.counts.messages },
    { k: 'meetings', label: 'Meetings', ic: 'cal', c: 'teal', n: d.counts.appointments }, { k: 'calls', label: 'AI calls', ic: 'mic', c: 'pink', n: d.counts.calls },
    { k: 'automations', label: 'Automations', ic: 'bot', c: 'pink', n: d.counts.automations },
  ]
  const tab = (tabs.find(t => t.k === sp.get('tab'))?.k || 'overview') as TabK
  const setTab = (t: TabK) => setSp(t === 'overview' ? {} : { tab: t })
  const isMaps = l.source === 'maps'
  const tel = telHref(l.phone)
  const wa = 'https://wa.me/' + d.wa_phone
  const ord = STATUSES.map(s => s.key)
  const ci = ord.indexOf(l.status)
  const back = l.is_lead ? <Link className="back" to="/leads">← Leads</Link> : <Link className="back" to={l.campaign_id ? '/campaigns/' + l.campaign_id : '/prospects'}>← {l.keyword}{d.location ? ' · ' + d.location : ''}</Link>
  return <>
    {back}
    <div className="card mt"><div className="lh"><span className={'avc c-' + avColor(nm)}>{nm.charAt(0).toUpperCase()}</span><div className="grow">
      <div className="row" style={{ gap: 8 }}><h2>{nm}</h2>{l.is_lead ? <StatusBadge s={l.status} /> : <span className="bdg c-gray"><Icon n="db" />Scraped</span>}{(isMaps || d.report) && <StageBadge s={l.stage} />}</div>
      <div className="meta">
        {l.contact_name && l.name !== l.contact_name && <span><Icon n="grid" />{l.name}</span>}
        {l.phone && <a href={tel}><Icon n="phone" />{l.phone}</a>}
        {l.email && <a href={'mailto:' + l.email}><Icon n="mail" />{l.email}</a>}
        {l.website && <a href={/^https?:\/\//i.test(l.website) ? l.website : 'https://' + l.website} target="_blank" rel="noopener noreferrer"><Icon n="globe" />{l.website.replace(/^https?:\/\/(www\.)?|\/$/g, '')}</a>}
        {l.rating ? <span><Icon n="star" />{l.rating} · {l.reviews || 0} reviews</span> : null}
        {l.maps_url && <a href={safeUrl(l.maps_url)} target="_blank" rel="noopener noreferrer"><Icon n="pin" />Google Maps{l.position ? ' #' + l.position : ''}</a>}
        <span><SrcBadge s={l.source} /></span>
      </div></div>
      <div className="pha">
        {l.phone && <><a className="btn" href={tel}><Icon n="phone" />Call</a><a className="btn btn-wa" href={wa} target="_blank" rel="noopener noreferrer"><Icon n="wa" />WhatsApp</a></>}
        {!l.is_lead && pro && <Btn v="n" ic="users" onClick={() => post('/leads/' + id + '/promote', {}, 'Moved to Leads.')}>Move to Leads</Btn>}
        {pro && adm ? <Btn v="p" ic="zap" onClick={() => setModal('run')}>{d.report ? 'Re-run audit' : 'Audit & report'}</Btn> : <Btn v="p" ic="cal" onClick={() => setModal('book')}>Book meeting</Btn>}
        <div className={'dd' + (dd ? ' open' : '')}><Btn onClick={() => setDd(!dd)}>More ▾</Btn><div className="ddm">
          <button onClick={() => { setDd(false); setModal('book') }}><Icon n="cal" />Book appointment</button>
          {d.booking_link && <button onClick={() => { setDd(false); CopyText(d.booking_link); toast('Booking link copied.') }}><Icon n="link" />Copy booking link</button>}
          {l.email && <button onClick={() => { setDd(false); setModal('email') }}><Icon n="mail" />Send email</button>}
          {l.phone && <button onClick={() => { setDd(false); setModal('wa') }}><Icon n="wa" />Send WhatsApp</button>}
          <button onClick={() => { setDd(false); setModal('call') }}><Icon n="mic" />AI voice call</button>
          {d.report && <><a href={d.report.url} target="_blank" rel="noopener noreferrer"><Icon n="eye" />View PDF report</a><button onClick={() => { setDd(false); CopyText(d.report!.url); toast('Report link copied.') }}><Icon n="link" />Copy report link</button></>}
          <hr />
          <button onClick={() => post('/leads/' + id + '/won')}><Icon n="check" />Mark as Won</button>
          <button onClick={() => post('/leads/' + id + '/lost')}><Icon n="x" />Not interested</button>
          {adm && <button className="red" onClick={del}><Icon n="trash" />Delete</button>}
        </div></div>
      </div></div></div>
    {d.busy ? <div className="al al-warn lp"><span className="spin" /><div className="grow"><b className="lp-t">{stageLabel(l.stage)} · {l.progress}%</b><div className="pb" style={{ margin: '6px 0 2px' }}><i style={{ width: l.progress + '%' }} /></div><small className="lp-m">{l.progress_msg}</small></div>
      <div className="lp-a"><Btn sm ic="activity" onClick={() => setModal('proc')}>View process</Btn>{adm && <Btn sm v="r" ic="stop" onClick={() => confirmDo('Stop this process now?') && post('/prospects/abort', { ids: [id] }, 'Process stopped.')}>Abort</Btn>}</div></div>
      : l.stage === 'failed' && l.last_error ? <div className="al al-err"><Icon n="alert" /><span className="grow"><b>Last process failed:</b> {l.last_error}</span><Btn sm ic="activity" onClick={() => setModal('proc')}>View process</Btn>{pro && adm && <Btn sm ic="refresh" onClick={() => setModal('run')}>Retry</Btn>}</div> : null}
    {l.is_lead ? <div className="pipe">{STATUSES.filter(s => s.key !== 'lost').map((s, i) => <div key={s.key} className={'ps ' + (l.status !== 'lost' && i <= ci ? 'done ' : '') + (l.status === s.key ? 'cur' : '')}>
      <button title={'Set status: ' + s.label} onClick={() => post('/leads/' + id + '/status', { status: s.key }, 'Status updated.')}><i>{l.status !== 'lost' && i < ci ? <Icon n="check" /> : i + 1}</i><span>{s.label}</span></button></div>)}
      {l.status === 'lost' && <div className="ps lost cur"><i>✕</i><span>Lost</span></div>}</div>
      : isMaps ? <MapsPipe l={l} report={!!d.report} /> : null}
    <div className="lt2"><nav className="ltab">{tabs.map(t => <a key={t.k} href="#" className={'c-' + t.c + (tab === t.k ? ' on' : '')} onClick={e => { e.preventDefault(); setTab(t.k) }}><Icon n={t.ic} />{t.label}{t.n ? <em>{t.n}</em> : null}</a>)}
      {pro && !l.is_lead && <Link className="c-orange" to={'/prospects/' + id}><Icon n="file" />Audit &amp; report</Link>}</nav>
      <div className="col">
        {tab === 'overview' && <Overview d={d} ev={ev.data || []} sym={sym} setTab={setTab} onNote={refresh} />}
        {tab === 'details' && <Details d={d} adm={adm} users={meta?.users || []} onSaved={refresh} />}
        {tab === 'journey' && <div className="card"><div className="card-h"><h3><Hi n="activity" c="violet" />Journey</h3><span className="bdg c-gray">{ev.data?.length ?? 0} events</span></div><NoteForm id={id} onDone={refresh} placeholder="Add a note to the journey…" label="Add note" /><Timeline list={ev.data || []} /></div>}
        {tab === 'messages' && <Messages d={d} onChange={refresh} openSend={setModal} />}
        {tab === 'meetings' && <Meetings d={d} openBook={() => setModal('book')} />}
        {tab === 'calls' && <Calls d={d} openCall={() => setModal('call')} />}
        {tab === 'automations' && <LeadAutomations leadId={id} />}
      </div></div>
    <BookModal open={modal === 'book'} d={d} onClose={() => setModal('')} onDone={refresh} />
    <CallModal open={modal === 'call'} d={d} onClose={() => setModal('')} onDone={refresh} />
    <EmailModal open={modal === 'email'} d={d} onClose={() => setModal('')} onDone={refresh} />
    <WaModal open={modal === 'wa'} d={d} onClose={() => setModal('')} onDone={refresh} cc={meta?.cc || '91'} />
    {pro && adm && <RunModal open={modal === 'run'} d={d} onClose={() => setModal('')} onDone={refresh} />}
    <ProcessModal open={modal === 'proc'} id={id} onClose={() => setModal('')} />
  </>
}

function MapsPipe({ l, report }: { l: LeadFull; report: boolean }) {
  const order: Record<string, number> = { new: 0, queued: 1, auditing: 1, reporting: 2, report_ready: 3, contacting: 3, contacted: 4, opened: 5, replied: 6, won: 7, lost: 7, failed: -1, skipped: -1 }
  let pos = order[l.stage] ?? 0
  if (l.map_score !== null) pos = Math.max(pos, 2)
  if (report) pos = Math.max(pos, 3)
  return <div className="pipe">{['Found', 'Queued', 'Audited', 'Report ready', 'Contacted', 'Opened', 'Replied', 'Won'].map((s, i) => <div key={s} className={'ps ' + (i <= pos ? 'done ' : '') + (i === pos ? 'cur' : '')}><i>{i < pos ? <Icon n="check" /> : i + 1}</i><span>{s}</span></div>)}</div>
}

export function Timeline({ list }: { list: Ev[] }) {
  if (!list.length) return <div className="empty sm"><Icon n="activity" /><p>No activity yet.</p></div>
  return <ul className="tl">{list.map(x => <li key={x.id} className={'ev-' + x.type}><i><Icon n={EVIC[x.type] || 'zap'} /></i><div><span>{x.message}</span><small>{dt(x.created_at)}{x.channel ? ' · ' + x.channel : ''}</small></div></li>)}</ul>
}

function NoteForm({ id, onDone, placeholder, label }: { id: number; onDone: () => void; placeholder: string; label: string }) {
  const [note, setNote] = useState('')
  const submit = async (e: FormEvent) => { e.preventDefault(); if (await act(() => api.post('/leads/' + id + '/note', { note }), 'Note added.')) { setNote(''); onDone() } }
  return <form className="row nw" onSubmit={submit}><input value={note} onChange={e => setNote(e.target.value)} placeholder={placeholder} style={{ margin: 0 }} required /><Btn v="n" type="submit" ic="plus">{label}</Btn></form>
}

function Overview({ d, ev, sym, setTab, onNote }: { d: LeadRes; ev: Ev[]; sym: string; setTab: (t: TabK) => void; onNote: () => void }) {
  const l = d.lead
  const fuLate = !!l.follow_up_at && new Date(l.follow_up_at).getTime() <= Date.now()
  const vm = (t: TabK, lb = 'View more') => <div className="vm"><a href="#" onClick={e => { e.preventDefault(); setTab(t) }}>{lb} →</a></div>
  const contact: [string, string][] = [['Contact', l.contact_name], ['Business', l.name], ['Phone', l.phone + (l.alt_phone ? ' · ' + l.alt_phone : '')], ['Email', l.email], ['Website', l.website], ['City', l.city], ['Address', l.address], ['Service', l.service], ['Tags', l.tags]]
  const score = (v: number | null, lb: string) => v === null ? null : <div className={v >= 75 ? 'c-green' : v >= 50 ? 'c-amber' : 'c-rose'}><span><Icon n="activity" />{lb}</span><b>{v} / 100</b><small>{v >= 75 ? 'Good' : v >= 50 ? 'Needs work' : 'Weak'}</small></div>
  return <>
    <div className="card"><div className="ov2">
      {l.is_lead ? <div className="c-violet"><span><Icon n="target" />Status</span><b>{statusOf(l.status).label}</b><small>{l.deal_value ? money(l.deal_value, sym) + ' deal' : 'No deal value yet'}</small></div>
        : <>{score(l.map_score, 'Google Maps')}{l.website ? score(l.web_score, 'Website') : <div className="c-rose"><span><Icon n="globe" />Website</span><b>None</b><small>Pitch a new website</small></div>}</>}
      <div className={fuLate ? 'c-rose' : 'c-orange'}><span><Icon n="clock" />Follow-up</span><b>{l.follow_up_at ? dDay(l.follow_up_at) : 'Not set'}</b><small>{l.follow_up_at ? (fuLate ? 'Overdue' : 'Scheduled') : 'Set one in Details'}</small></div>
      <div className="c-teal"><span><Icon n="cal" />Next meeting</span><b>{d.next_appt ? dDay(d.next_appt.start_at) : 'None booked'}</b><small>{d.next_appt ? d.next_appt.title : 'Book or share the booking link'}</small></div>
      <div className="c-blue"><span><Icon n="user" />Owner</span><b>{d.owner || 'Unassigned'}</b><small>{(SOURCES[l.source] || [l.source])[0]}</small></div>
      <div className="c-green"><span><Icon n="msg" />Last reply</span><b>{l.replied_at ? l.replied_via.charAt(0).toUpperCase() + l.replied_via.slice(1) : 'No reply yet'}</b><small>{l.replied_at ? ago(l.replied_at) : 'Email ' + (l.email_status || '—') + ' · WhatsApp ' + (l.wa_status || '—')}</small></div>
      <div className="c-indigo"><span><Icon n="activity" />Last activity</span><b>{ago(l.last_activity || l.created_at)}</b><small>added {date(l.created_at)}</small></div>
    </div></div>
    <div className="g2">
      <div className="card"><div className="card-h"><h3><Hi n="user" c="blue" />Contact</h3><a className="lnk sm" href="#" onClick={e => { e.preventDefault(); setTab('details') }}><Icon n="edit" />Edit</a></div>
        <div className="dl" style={{ gridTemplateColumns: '1fr' }}>
          {contact.filter(([, v]) => (v || '').replace(/[ ·]/g, '') !== '').map(([k, v]) => <div key={k}><span>{k}</span><b>{v}</b></div>)}
          {d.fields.filter(f => String(l.custom?.[f.fkey] ?? '') !== '').map(f => <div key={f.id}><span>{f.label}</span><b>{String(l.custom[f.fkey])}</b></div>)}
        </div></div>
      <div className="col">
        {l.message && <div className="card"><div className="card-h"><h3><Hi n="msg" c="amber" />Enquiry</h3></div><p className="sm" style={{ whiteSpace: 'pre-wrap' }}>{l.message.length > 320 ? l.message.slice(0, 319) + '…' : l.message}</p>{l.message.length > 320 && vm('details', 'Read full enquiry')}</div>}
        <div className="card"><div className="card-h"><h3><Hi n="edit" c="violet" />Quick note</h3></div><NoteForm id={l.id} onDone={onNote} placeholder="Called, discussed pricing…" label="Add" />
          {l.notes && <p className="hint" style={{ whiteSpace: 'pre-wrap' }}><b>Internal notes:</b> {l.notes.length > 220 ? l.notes.slice(0, 219) + '…' : l.notes}</p>}</div>
      </div>
    </div>
    <div className="card"><div className="card-h"><h3><Hi n="activity" c="violet" />Recent activity</h3><span className="bdg c-gray">{d.counts.journey} events</span></div><Timeline list={ev.slice(0, 5)} />{ev.length > 5 && vm('journey', 'View full journey')}</div>
  </>
}

function Details({ d, adm, users, onSaved }: { d: LeadRes; adm: boolean; users: { id: number; name: string }[]; onSaved: () => void }) {
  const l = d.lead
  const init = () => ({
    status: l.status, assigned_to: l.assigned_to ? String(l.assigned_to) : '', follow_up_at: toLocalInput(l.follow_up_at), service: l.service, deal_value: l.deal_value === null ? '' : String(l.deal_value), tags: l.tags,
    contact_name: l.contact_name, phone: l.phone, alt_phone: l.alt_phone, email: l.email, name: l.name, website: l.website, city: l.city, category: l.category, address: l.address, message: l.message, notes: l.notes,
  })
  const [v, setV] = useState<Record<string, string>>(init)
  const [cf, setCf] = useState<Record<string, string>>(() => Object.fromEntries(d.fields.map(f => [f.fkey, String(l.custom?.[f.fkey] ?? '')])))
  const [busy, setBusy] = useState(false)
  useEffect(() => { setV(init()) }, [l.id])
  const set = (k: string) => (e: { target: { value: string } }) => setV(s => ({ ...s, [k]: e.target.value }))
  const save = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true)
    const body: Record<string, unknown> = { ...v, follow_up_at: localToISO(v.follow_up_at), custom: cf }
    if (!adm) delete body.assigned_to
    if (!l.is_lead) delete body.status
    try { await api.put('/leads/' + l.id, body); toast('Saved.'); onSaved() } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const inp = (k: string, label: ReactNode, type = 'text') => <label key={k}>{label}<input type={type} value={v[k]} onChange={set(k)} /></label>
  const extra = l.custom?._extra && typeof l.custom._extra === 'object' ? Object.entries(l.custom._extra as Record<string, unknown>) : []
  return <form className="card" onSubmit={save}>
    <div className="card-h"><h3><Hi n="edit" c="blue" />Lead details</h3><Btn sm v="n" type="submit" ic="check" busy={busy}>Save</Btn></div>
    <div className="sec"><h3>Status &amp; deal</h3></div>
    <div className="fg3">
      {l.is_lead && <label>Status<select value={v.status} onChange={set('status')}>{STATUSES.map(s => <option key={s.key} value={s.key}>{s.label}</option>)}</select></label>}
      {adm && users.length > 0 && <label>Owner<select value={v.assigned_to} onChange={set('assigned_to')}><option value="">Unassigned</option>{users.map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select></label>}
      {inp('follow_up_at', 'Follow-up', 'datetime-local')}{inp('service', 'Service')}
      <label>Deal value<input type="number" step="0.01" value={v.deal_value} onChange={set('deal_value')} /></label>
      <label>Tags<input value={v.tags} onChange={set('tags')} placeholder="hot, referral" /></label>
    </div>
    <div className="sec mt"><h3>Contact</h3></div>
    <div className="fg3">{inp('contact_name', 'Contact person')}{inp('phone', <>Phone {l.phone_source && <em className="src">{l.phone_source}</em>}</>)}{inp('alt_phone', 'Alt. phone')}{inp('email', 'Email', 'email')}{inp('name', 'Business')}{inp('website', 'Website')}{inp('city', 'City')}{inp('category', 'Category')}{inp('address', 'Address')}</div>
    {d.fields.length > 0 && <><div className="sec mt"><h3>More fields</h3></div><div className="fg3">{d.fields.map(f => <label key={f.id} className={f.type === 'textarea' ? 'full' : ''}>{f.label}<CustomInput f={f} value={cf[f.fkey] || ''} onChange={x => setCf(s => ({ ...s, [f.fkey]: x }))} /></label>)}</div></>}
    <div className="sec mt"><h3>Enquiry &amp; notes</h3></div>
    <div className="fg"><label>Enquiry message<textarea rows={4} value={v.message} onChange={set('message')} /></label><label>Internal notes<textarea rows={4} value={v.notes} onChange={set('notes')} /></label></div>
    {extra.length > 0 && <p className="hint">Source data: {extra.map(([k, x]) => k + ': ' + (typeof x === 'object' ? JSON.stringify(x) : String(x))).join(' · ')}</p>}
    <div className="card-f"><Btn v="p" type="submit" ic="check" busy={busy}>Save changes</Btn></div>
  </form>
}

function Messages({ d, onChange, openSend }: { d: LeadRes; onChange: () => void; openSend: (m: string) => void }) {
  const l = d.lead
  const q = useSection<any>(['lead', l.id, 'messages'], '/leads/' + l.id + '/messages')
  const [via, setVia] = useState('whatsapp')
  const [msg, setMsg] = useState('')
  const msgs = asRows(q.data)
  const logReply = async (e: FormEvent) => {
    e.preventDefault()
    const r = await act(() => api.post<any>('/leads/' + l.id + '/replied', { via, msg }))
    if (r) { toast(r.message || 'Reply logged.'); setMsg(''); onChange() }
  }
  return <>
    <div className="g2">
      <div className="card"><div className="card-h"><h3><Hi n="send" c="amber" />Status</h3><div className="row">{l.email && <Btn sm ic="mail" onClick={() => openSend('email')}>Email</Btn>}{l.phone && <Btn sm ic="wa" onClick={() => openSend('wa')}>WhatsApp</Btn>}</div></div>
        <div className="chs"><div><span><Icon n="mail" />Email</span><ChBadge s={l.email_status} /></div><div><span><Icon n="wa" />WhatsApp</span><ChBadge s={l.wa_status} /></div>
          <div><span><Icon n="msg" />Reply</span>{l.replied_at ? <span className="bdg c-green">{l.replied_via.charAt(0).toUpperCase() + l.replied_via.slice(1)} · {ago(l.replied_at)}</span> : <span className="muted sm">No reply yet</span>}</div>
          {d.report && <div><span><Icon n="eye" />Report opened</span><b>{d.report.views}×</b></div>}</div></div>
      <form className="card" onSubmit={logReply}><div className="card-h"><h3><Hi n="check" c="green" />Log a reply</h3></div>
        <label>Channel<select value={via} onChange={e => setVia(e.target.value)}><option value="whatsapp">WhatsApp</option><option value="email">Email</option><option value="call">Phone call</option><option value="other">Other</option></select></label>
        <label className="mb0">What did they say?<input value={msg} onChange={e => setMsg(e.target.value)} placeholder="Optional" /></label>
        <div className="card-f"><Btn v="n" type="submit" ic="check">Log reply</Btn></div></form>
    </div>
    <div className="card"><div className="card-h"><h3><Hi n="msg" c="green" />Messages</h3></div>
      {q.isLoading ? <Spinner /> : !msgs.length ? <div className="empty sm"><Icon n="msg" /><p>No messages yet.</p></div> :
        msgs.map((m: any) => <div key={m.id} className={'msg msg-' + m.direction}><div className="msg-h"><Icon n={m.channel === 'email' ? 'mail' : 'wa'} /><b>{String(m.channel || '').charAt(0).toUpperCase() + String(m.channel || '').slice(1)}{m.direction === 'in' ? ' · received' : ''}</b><ChBadge s={m.status} /><small>{ago(m.created_at)}</small></div>
          {m.subject && <b className="sm">{m.subject}</b>}<p style={{ whiteSpace: 'pre-wrap' }}>{m.body}</p></div>)}
    </div>
  </>
}

const APPT: Record<string, string> = { scheduled: 'c-blue', completed: 'c-green', no_show: 'c-orange', cancelled: 'c-gray' }

function Meetings({ d, openBook }: { d: LeadRes; openBook: () => void }) {
  const l = d.lead, qc = useQueryClient()
  const q = useSection<any>(['lead', l.id, 'appointments'], '/leads/' + l.id + '/appointments')
  const rows = asRows(q.data)
  const setSt = async (aid: number, status: string) => { if (await act(() => api.post('/appointments/' + aid + '/status', { status }), 'Updated.')) qc.invalidateQueries({ queryKey: ['lead', l.id] }) }
  return <div className="card"><div className="card-h"><h3><Hi n="cal" c="teal" />Meetings</h3><div className="row">{d.booking_link && <Btn sm ic="link" onClick={() => { CopyText(d.booking_link); toast('Booking link copied.') }}>Copy booking link</Btn>}<Btn sm v="p" ic="plus" onClick={openBook}>Book</Btn></div></div>
    {q.isLoading ? <Spinner /> : !rows.length ? <div className="empty sm"><Icon n="cal" /><p>No meetings yet. Book one or send their personal booking link.</p></div> :
      rows.map((ap: any) => { const s = new Date(ap.start_at); return <div key={ap.id} className="li"><span className="when c-teal"><b>{String(s.getDate()).padStart(2, '0')}</b><small>{s.toLocaleString('en-GB', { month: 'short' })}</small></span>
        <div className="grow"><b>{ap.title}</b><small>{s.toLocaleString('en-GB', { weekday: 'short', hour: '2-digit', minute: '2-digit', hour12: true })}{ap.meet_link && <> · <a className="lnk" href={safeUrl(ap.meet_link)} target="_blank" rel="noopener noreferrer">Google Meet</a></>}</small></div>
        <span className={'bdg ' + (APPT[ap.status] || 'c-gray')}>{String(ap.status).replace('_', ' ').replace(/^./, (c: string) => c.toUpperCase())}</span>
        {ap.status === 'scheduled' && <span className="row" style={{ gap: 4 }}><Btn xs ic="check" onClick={() => setSt(ap.id, 'completed')}>Done</Btn><Btn xs onClick={() => setSt(ap.id, 'no_show')}>No-show</Btn><Btn xs onClick={() => setSt(ap.id, 'cancelled')}>Cancel</Btn></span>}
      </div> })}
  </div>
}

function Calls({ d, openCall }: { d: LeadRes; openCall: () => void }) {
  const l = d.lead
  const q = useSection<any>(['lead', l.id, 'calls'], '/leads/' + l.id + '/calls')
  const rows = asRows(q.data)
  const mmss = (n: number) => String(Math.floor(n / 60)).padStart(2, '0') + ':' + String(n % 60).padStart(2, '0')
  return <div className="card"><div className="card-h"><h3><Hi n="mic" c="pink" />AI calls</h3><Btn sm v="p" ic="phone" disabled={!l.phone} onClick={openCall}>Call now</Btn></div>
    {q.isLoading ? <Spinner /> : !rows.length ? <div className="empty sm"><Icon n="mic" /><p>No calls yet.</p></div> :
      rows.map((c: any) => <div key={c.id} className="msg"><div className="msg-h"><Icon n="mic" /><b>{String(c.provider || '').replace(/^./, (x: string) => x.toUpperCase())}</b><span className={'bdg ' + (['completed', 'ended', 'done'].includes(c.status) ? 'c-green' : c.status === 'failed' ? 'c-rose' : 'c-blue')}>{c.status}</span><small>{ago(c.created_at)}{c.duration ? ' · ' + mmss(c.duration) : ''}</small></div>
        {c.summary && <p>{c.summary}</p>}{c.recording && <a className="lnk" href={safeUrl(c.recording)} target="_blank" rel="noopener noreferrer"><Icon n="play" />Recording</a>}
        {c.transcript && <details><summary className="sm muted">Transcript</summary><p style={{ whiteSpace: 'pre-wrap' }}>{c.transcript}</p></details>}</div>)}
  </div>
}

// ------------------------------------------------------------------ action modals (other modules' endpoints)
type MP = { open: boolean; d: LeadRes; onClose: () => void; onDone: () => void }

function useSend(onClose: () => void, onDone: () => void) {
  const [busy, setBusy] = useState(false)
  const send = async (url: string, body: unknown, ok: string) => {
    setBusy(true)
    try { const r = await api.post<any>(url, body); toast(r?.message || ok); if (r?.link) window.open(r.link, '_blank', 'noopener'); onClose(); onDone() } catch (e: any) { toast(e.status === 404 ? 'This feature is not available yet.' : e.message) } finally { setBusy(false) }
  }
  return { busy, send }
}

function BookModal({ open, d, onClose, onDone }: MP) {
  const [at, setAt] = useState(''), [notes, setNotes] = useState('')
  const { busy, send } = useSend(onClose, onDone)
  const submit = (e: FormEvent) => { e.preventDefault(); if (!at) return toast('Pick a date and time.'); send('/appointments', { lead_id: d.lead.id, at: localToISO(at), notes }, 'Appointment booked.') }
  return <Modal open={open} onClose={onClose} title="Book appointment" ic="cal" c="teal"><form onSubmit={submit}>
    <label>Date &amp; time<input type="datetime-local" required value={at} min={toLocalInput(new Date().toISOString())} onChange={e => setAt(e.target.value)} /></label>
    <label>Notes<textarea rows={2} value={notes} onChange={e => setNotes(e.target.value)} /></label>
    {d.booking_link && <p className="hint">Or let them pick a time: <a className="lnk" href="#" onClick={e => { e.preventDefault(); CopyText(d.booking_link); toast('Booking link copied.') }}>copy booking link</a></p>}
    <div className="md-f"><Btn v="p" type="submit" ic="check" busy={busy}>Book</Btn></div></form></Modal>
}

function CallModal({ open, d, onClose, onDone }: MP) {
  const [first, setFirst] = useState(''), [task, setTask] = useState('')
  const { busy, send } = useSend(onClose, onDone)
  return <Modal open={open} onClose={onClose} title="AI voice call" ic="mic" c="pink"><form onSubmit={e => { e.preventDefault(); send('/leads/' + d.lead.id + '/call', { first_message: first, task }, 'AI call started – the result will appear in the journey.') }}>
    <p className="muted">The AI calls <b>{d.lead.phone || '—'}</b>. Leave blank to use the default script from Settings.</p>
    <label>First sentence<input value={first} onChange={e => setFirst(e.target.value)} /></label>
    <label>Task / script for this call<textarea rows={4} value={task} onChange={e => setTask(e.target.value)} /></label>
    <div className="md-f"><Btn v="p" type="submit" ic="phone" busy={busy} disabled={!d.lead.phone}>Start call</Btn></div></form></Modal>
}

function EmailModal({ open, d, onClose, onDone }: MP) {
  const [subject, setSubject] = useState(''), [body, setBody] = useState(''), [att, setAtt] = useState(!!d.report)
  const { busy, send } = useSend(onClose, onDone)
  return <Modal open={open} onClose={onClose} title="Send email" ic="mail" c="blue"><form onSubmit={e => { e.preventDefault(); send('/leads/' + d.lead.id + '/email', { subject, body, attach_report: att }, 'Email sent.') }}>
    <p className="muted sm">To <b>{d.lead.email}</b>. You can use variables like {'{{contact_name}}'} and {'{{booking_link}}'}.</p>
    <label>Subject<input required value={subject} onChange={e => setSubject(e.target.value)} /></label>
    <label>Message<textarea rows={6} required value={body} onChange={e => setBody(e.target.value)} /></label>
    {d.report && <label className="chk"><input type="checkbox" checked={att} onChange={e => setAtt(e.target.checked)} /> Attach the PDF report</label>}
    <div className="md-f"><Btn v="p" type="submit" ic="send" busy={busy}>Send</Btn></div></form></Modal>
}

function WaModal({ open, d, onClose, onDone, cc }: MP & { cc: string }) {
  const [message, setMessage] = useState(''), [att, setAtt] = useState(!!d.report)
  const { busy, send } = useSend(onClose, onDone)
  return <Modal open={open} onClose={onClose} title="Send WhatsApp" ic="wa" c="green"><form onSubmit={e => { e.preventDefault(); send('/leads/' + d.lead.id + '/whatsapp', { message, attach_report: att }, 'WhatsApp sent.') }}>
    <p className="muted sm">To <b>{d.lead.phone}</b>. Without a WhatsApp API, WhatsApp opens with the message ready.</p>
    <label>Message<textarea rows={5} required value={message} onChange={e => setMessage(e.target.value)} /></label>
    {d.report && <label className="chk"><input type="checkbox" checked={att} onChange={e => setAtt(e.target.checked)} /> Attach the PDF report</label>}
    <div className="md-f"><a className="btn" href={waLink(d.lead.phone, cc, message)} target="_blank" rel="noopener noreferrer"><Icon n="wa" />Open WhatsApp</a><Btn v="p" type="submit" ic="send" busy={busy}>Send</Btn></div></form></Modal>
}

function RunModal({ open, d, onClose, onDone }: MP) {
  const [auto, setAuto] = useState(false)
  const { busy, send } = useSend(onClose, onDone)
  const l = d.lead
  return <Modal open={open} onClose={onClose} title="Audit & report" ic="zap" c="violet">
    <p className="muted sm">Checks the {l.website ? 'website and ' : ''}Google Maps listing, researches the market and writes a branded PDF report.</p>
    <label className="chk"><input type="checkbox" checked={auto} onChange={e => setAuto(e.target.checked)} /> Send by Email &amp; WhatsApp when the report is ready</label>
    <div className="md-f">{l.map_score !== null && <Btn ic="file" busy={busy} onClick={() => send('/leads/' + l.id + '/report', { contact: auto }, 'Report regeneration queued.')}>Only rewrite report</Btn>}
      <Btn v="p" ic="play" busy={busy} onClick={() => send('/prospects/process', { ids: [l.id], steps: auto ? ['audit', 'report', 'contact'] : ['audit', 'report'], auto }, 'Audit & report started – watch the progress here.')}>Full audit + report</Btn></div>
  </Modal>
}

function ProcessModal({ open, id, onClose }: { open: boolean; id: number; onClose: () => void }) {
  const q = useQuery({ queryKey: ['lead', id, 'process'], queryFn: () => api.get<any>('/leads/' + id + '/process').catch(() => null), enabled: open, refetchInterval: open ? 4000 : false })
  const p = q.data
  const steps: any[] = p?.steps || []
  return <Modal open={open} onClose={onClose} title="Process log" ic="activity" c="violet" size="lg">
    {q.isLoading ? <Spinner /> : !p ? <div className="empty sm"><Icon n="activity" /><p>No process log for this lead.</p></div> : <>
      <p className="sm"><b>{p.stage_label}</b> · {p.progress}% {p.msg && <span className="muted">· {p.msg}</span>}</p>
      {p.error && <Alert t="err">{p.error}</Alert>}
      {!steps.length ? <p className="muted sm">No steps recorded yet.</p> :
        <ul className="tl">{steps.map((s: any, i: number) => <li key={s.id || i} className={s.status === 'err' ? 'ev-error' : s.status === 'ok' ? 'ev-sent' : 'ev-note'}><i><Icon n={s.kind === 'ai' ? 'spark' : s.status === 'err' ? 'alert' : s.status === 'ok' ? 'check' : 'clock'} /></i>
          <div><span>{s.step}</span><small>{[s.model, s.ms ? (s.ms / 1000).toFixed(1) + 's' : '', s.detail].filter(Boolean).join(' · ')}</small></div></li>)}</ul>}
    </>}
  </Modal>
}
