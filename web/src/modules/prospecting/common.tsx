// Shared pieces of the Prospecting pages: business table, bulk bar with Initiate Process, process log.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { dateTime } from '@/lib/format'
import { Badge, Btn, Empty, Icon, Modal, Spinner, confirmDo, toast } from '@/components/ui'

export interface Row {
  id: number; position: number; name: string; category: string; city: string; phone: string; phone_source: string; email: string; website: string
  rating: number | null; reviews: number | null; web_score: number | null; map_score: number | null; stage: string; progress: number; progress_msg: string
  last_error: string; email_status: string; wa_status: string; replied_at: string | null; replied_via: string; is_lead: boolean; campaign_id: number
}
export interface Page { rows: Row[]; total: number; page: number; pages: number; per: number }

/** Filter sent with "all matching" bulk actions (same keys as the list API). */
export interface ListFilter { campaign_id?: number; f?: string; q?: string; stage?: string; cid?: number; web?: string; il?: string }

export const STAGES: Record<string, [string, string]> = { new: ['New', 'gray'], queued: ['Queued', 'blue'], auditing: ['Auditing', 'blue'], reporting: ['Writing report', 'purple'], report_ready: ['Report ready', 'gold'], contacting: ['Sending', 'blue'], contacted: ['Contacted', 'teal'], opened: ['Opened', 'teal'], replied: ['Replied', 'green'], won: ['Won', 'green'], lost: ['Not interested', 'red'], failed: ['Failed', 'red'], skipped: ['Skipped', 'gray'] }
export const BUSY = ['queued', 'auditing', 'reporting', 'contacting']
const CH: Record<string, string> = { sent: 'teal', delivered: 'teal', read: 'green', opened: 'green', failed: 'red', manual: 'gold', queued: 'blue' }
const CB: Record<string, [string, string]> = { searching: ['Searching', 'blue'], ready: ['Ready', 'green'], error: ['Error', 'red'], stopped: ['Stopped', 'gray'] }

export function StageBadge({ s }: { s: string }) { const a = STAGES[s] || [s ? s[0].toUpperCase() + s.slice(1) : '', 'gray']; return <Badge c={a[1]}>{a[0]}</Badge> }
export function CampBadge({ s }: { s: string }) { const a = CB[s] || [s, 'gray']; return <Badge c={a[1]}>{a[0]}</Badge> }
function ChBadge({ s }: { s: string }) { return s ? <Badge c={CH[s] || 'gray'}>{s[0].toUpperCase() + s.slice(1)}</Badge> : <span className="muted">—</span> }
function Score({ v }: { v: number | null }) { return v === null || v === undefined ? <span className="muted">—</span> : <span className={'sc sc-' + (v >= 75 ? 'g' : v >= 50 ? 'a' : 'r')}>{v}</span> }
const strim = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1) + '…' : s)
const host = (u: string) => strim(u.replace(/^https?:\/\/(www\.)?|\/$/g, ''), 32)
const norm = (u: string) => (/^https?:\/\//i.test(u) ? u : 'http://' + u)

export function useSel(rows: Row[]) {
  const [sel, setSel] = useState<Set<number>>(new Set())
  const [all, setAll] = useState(false)
  return {
    sel, all, ids: [...sel],
    toggle: (id: number) => { setAll(false); setSel(s => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n }) },
    page: (on: boolean) => { setAll(false); setSel(on ? new Set(rows.map(r => r.id)) : new Set()) },
    pick: (ids: number[]) => { setAll(false); setSel(new Set(ids)) },
    setAll: (on: boolean) => setAll(on),
    clear: () => { setAll(false); setSel(new Set()) },
  }
}
export type Sel = ReturnType<typeof useSel>

export function LeadsTable({ rows, sel, offset, emptyMsg, onAbort, onProc }: { rows: Row[]; sel: Sel; offset: number; emptyMsg: string; onAbort: (ids: number[]) => void; onProc: (id: number) => void }) {
  if (!rows.length) return <div className="empty"><Icon n="db" c="eic" /><h4>Nothing here yet</h4><p>{emptyMsg}</p></div>
  const allOn = rows.length > 0 && rows.every(r => sel.sel.has(r.id))
  return <div className="tw"><table className="tbl lt"><thead><tr><th className="cb"><input type="checkbox" checked={allOn} onChange={e => sel.page(e.target.checked)} /></th><th className="sn">#</th><th>Business</th><th>Contact</th><th>Rating</th><th title="Website score">Web</th><th title="Google Maps score">Maps</th><th>Status / progress</th><th>Email</th><th>WhatsApp</th><th>Reply</th><th></th></tr></thead>
    <tbody>{rows.map((l, i) => {
      const busy = BUSY.includes(l.stage)
      return <tr key={l.id} data-stage={l.stage}>
        <td className="cb"><input type="checkbox" checked={sel.sel.has(l.id)} onChange={() => sel.toggle(l.id)} /></td>
        <td className="sn">{offset + i + 1}</td>
        <td className="biz"><Link to={'/leads/' + l.id}><b>{l.name}</b></Link><small>{l.category}{l.city && ' · ' + l.city}{l.position > 0 && ' · #' + l.position + ' on Maps'}</small>
          {l.website ? <a className="wl" href={norm(l.website)} target="_blank" rel="noopener noreferrer"><Icon n="globe" />{host(l.website)}</a> : <span className="bdg bdg-red sm">No website</span>}
          {l.is_lead && <> <span className="bdg bdg-green sm"><Icon n="check" />In Leads</span></>}</td>
        <td className="ctc">{l.phone && <span><Icon n="phone" />{l.phone}{l.phone_source === 'website' && <em className="src" title="Found on website">web</em>}</span>}
          {l.email && <span><Icon n="mail" />{strim(l.email, 30)}</span>}{!l.phone && !l.email && <span className="muted">—</span>}</td>
        <td className="nowrap">{l.rating ? <><b>{l.rating}</b>★ <small className="muted">({l.reviews || 0})</small></> : <span className="muted">—</span>}</td>
        <td><Score v={l.web_score} /></td><td><Score v={l.map_score} /></td>
        <td><StageBadge s={l.stage} />
          {busy ? <div className="prg"><div className="pb"><i style={{ width: l.progress + '%' }} /></div><small className="pbm" title={l.progress_msg}>{l.progress}% · {strim(l.progress_msg || '', 60)}</small>
            <span className="prg-a"><button type="button" className="lnk" onClick={() => onProc(l.id)}>View process</button><button type="button" className="btn btn-xs btn-r" onClick={() => onAbort([l.id])} title="Stop this process"><Icon n="stop" />Abort</button></span></div>
            : l.stage === 'failed' && l.last_error ? <><span className="err-t" title={l.last_error}>{strim(l.last_error, 90)}</span> <button type="button" className="lnk sm" onClick={() => onProc(l.id)}>View process</button></> : null}</td>
        <td><ChBadge s={l.email_status} /></td><td><ChBadge s={l.wa_status} /></td>
        <td>{l.replied_at ? <span className="bdg bdg-green"><Icon n="check" />{l.replied_via ? l.replied_via[0].toUpperCase() + l.replied_via.slice(1) : 'Yes'}</span> : <span className="muted">—</span>}</td>
        <td className="act"><Link className="ib" to={'/leads/' + l.id} title="Open"><Icon n="eye" /></Link></td>
      </tr>
    })}</tbody></table></div>
}

/** POST /prospects/bulk – selected ids or every record matching a filter. */
export async function runBulk(action: string, target: { ids?: number[]; all?: boolean; filter?: ListFilter }, extra: Record<string, unknown> = {}) {
  try {
    const r = await api.post<{ message: string }>('/prospects/bulk', { action, ids: target.ids || [], all: !!target.all, filter: target.filter, ...extra })
    toast(r.message)
    return true
  } catch (e: any) { toast(e.message); return false }
}

interface Prompt { id: number; name: string; is_active: boolean }

/** Bulk actions over the selected rows (or every matching record) – v1 bulkbar.php. */
export function BulkBar({ sel, total, filter, onDone, initOpen, setInitOpen }: { sel: Sel; total: number; filter: ListFilter; onDone: () => void; initOpen: boolean; setInitOpen: (v: boolean) => void }) {
  const [dd, setDd] = useState(false)
  const n = sel.all ? total : sel.ids.length
  const run = async (action: string, ask?: string, extra: Record<string, unknown> = {}) => {
    setDd(false)
    if (!n) { toast('Select at least one lead.'); return }
    if (ask && !confirmDo(ask)) return
    if (await runBulk(action, sel.all ? { all: true, filter } : { ids: sel.ids }, extra)) { sel.clear(); onDone() }
  }
  const pageAll = sel.ids.length > 0 && !sel.all && total > sel.ids.length
  return <>
    <div className={'bulk' + (n ? '' : ' idle')}>
      <span className="bsel"><b>{n.toLocaleString('en-IN')}</b><span className="bs-t"> selected</span></span>
      {pageAll && <button type="button" className="lnk sm" onClick={() => sel.setAll(true)}>Select all {total.toLocaleString('en-IN')} matching</button>}
      <Btn v="y" sm ic="zap" onClick={() => n ? setInitOpen(true) : toast('Select at least one lead.')}>Initiate Process</Btn>
      <Btn sm ic="send" onClick={() => run('contact', 'Send email & WhatsApp to the selected businesses that have a report?')}>Send outreach</Btn>
      <Btn sm ic="users" onClick={() => run('promote', 'Move the selected businesses to Leads (CRM)?')}>Move to Leads</Btn>
      <div className={'dd' + (dd ? ' open' : '')}><button type="button" className="btn btn-sm" onClick={() => setDd(!dd)}>More ▾</button>
        <div className="ddm">
          <button type="button" onClick={() => run('abort', 'Stop processing the selected businesses?')}><Icon n="stop" />Abort processing</button>
          <button type="button" onClick={() => run('won')}><Icon n="check" />Mark as Won (moves to Leads)</button>
          <button type="button" onClick={() => run('lost')}><Icon n="x" />Mark as Not interested</button><hr />
          <button type="button" className="red" onClick={() => run('delete', 'Delete the selected records and their history?')}><Icon n="trash" />Delete</button>
        </div></div>
    </div>
    {initOpen && <InitModal n={n} onClose={() => setInitOpen(false)} onStart={async (o) => { setInitOpen(false); await run('initiate', undefined, o) }} />}
  </>
}

export function InitModal({ n, onClose, onStart }: { n: number; onClose: () => void; onStart: (o: { pw: number; pn: number; auto: boolean }) => void }) {
  const q = useQuery({ queryKey: ['prospecting', 'report-prompts'], queryFn: () => api.get<{ website: Prompt[]; noweb: Prompt[]; auto_contact: boolean }>('/prospecting/report-prompts') })
  const [pw, setPw] = useState(0), [pn, setPn] = useState(0), [auto, setAuto] = useState<boolean | null>(null)
  const d = q.data
  const a = auto ?? !!d?.auto_contact
  return <Modal open onClose={onClose} title="Initiate Process" ic="zap" c="orange" footer={<><Btn onClick={onClose}>Cancel</Btn><Btn v="y" ic="play" disabled={!d} onClick={() => onStart({ pw: pw || d?.website[0]?.id || 0, pn: pn || d?.noweb[0]?.id || 0, auto: a })}>Start</Btn></>}>
    {!d ? <Spinner /> : <>
      <p className="muted">For each selected business Setu will: audit the website and Google Maps listing → research &amp; write the AI report → design the PDF{a && ' → send it by Email & WhatsApp'}. You can watch progress or stop it any time from <b>Activity</b>.</p>
      <p className="sm">{n.toLocaleString('en-IN')} business{n === 1 ? '' : 'es'} selected.</p>
      <label>Report prompt – business has a website<select value={pw || d.website[0]?.id || ''} onChange={e => setPw(Number(e.target.value))}>{d.website.map(p => <option key={p.id} value={p.id}>{p.name}{p.is_active ? ' (in use)' : ''}</option>)}</select></label>
      <label>Report prompt – no website<select value={pn || d.noweb[0]?.id || ''} onChange={e => setPn(Number(e.target.value))}>{d.noweb.map(p => <option key={p.id} value={p.id}>{p.name}{p.is_active ? ' (in use)' : ''}</option>)}</select></label>
      <label className="chk"><input type="checkbox" checked={a} onChange={e => setAuto(e.target.checked)} /> Send by Email &amp; WhatsApp automatically when the report is ready</label>
    </>}
  </Modal>
}

interface Step { id?: number; step: string; status: string; detail?: string; ms?: number; model?: string; kind?: string; created_at?: string }

/** "View process" – the step log of a lead's background work (GET /api/leads/{id}/process, ai-engine). */
export function ProcessModal({ id, onClose }: { id: number; onClose: () => void }) {
  const q = useQuery({ queryKey: ['process', id], queryFn: () => api.get<any>('/leads/' + id + '/process'), refetchInterval: 3000 })
  const steps: Step[] = Array.isArray(q.data) ? q.data : q.data?.steps || []
  const ic: Record<string, [string, string]> = { ok: ['check', 'green'], err: ['x', 'red'], warn: ['alert', 'gold'], run: ['clock', 'blue'] }
  return <Modal open onClose={onClose} title="Process" ic="activity" size="lg">
    {q.isLoading ? <Spinner /> : q.error ? <Empty ic="activity" title="Process log not available">{(q.error as Error).message}</Empty> : !steps.length ? <Empty ic="activity" title="No steps yet" /> :
      <div className="tw"><table className="tbl"><tbody>{steps.map((s, i) => { const x = ic[s.status] || ic.run; return <tr key={s.id ?? i}>
        <td style={{ width: 28 }}><Badge c={x[1]}><Icon n={x[0]} /></Badge></td>
        <td>{s.kind === 'start' ? <b>{s.step}</b> : s.step}{s.detail && <><br /><small className="muted">{s.detail}</small></>}</td>
        <td className="nowrap"><small className="muted">{s.ms ? (s.ms / 1000).toFixed(1) + 's' : ''}{s.created_at && ' · ' + dateTime(s.created_at)}</small></td>
      </tr> })}</tbody></table></div>}
  </Modal>
}
