import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useCan, useMe } from '@/lib/session'
import { safeUrl } from '@/lib/format'
import { Alert, Badge, Btn, Card, Empty, Field, Icon, Kpi, Modal, Pager, Spinner, Tabs, act, confirmDo, toast, usePageTitle } from '@/components/ui'
import { Appt, ApptPage, ApptStatus, STATUS, Summary, addDays, copy, who } from './api'

type View = 'calendar' | 'list'

export default function Appointments() {
  usePageTitle('Appointments', 'Meetings booked by leads or your team')
  const [sp, setSp] = useSearchParams()
  const qc = useQueryClient()
  const can = useCan()
  const sum = useQuery({ queryKey: ['appt-summary'], queryFn: () => api.get<Summary>('/appointments/summary') })
  const view: View = sp.get('view') === 'list' ? 'list' : 'calendar'
  const [add, setAdd] = useState(false)
  const [edit, setEdit] = useState<Appt | null>(null)
  const pre = Number(sp.get('lead') || 0)

  // ?add=1 (quick-add menu) or ?lead=ID (from a lead) opens the booking form
  useEffect(() => {
    if (sp.get('add') || pre) setAdd(true)
  }, [])

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['appt-summary'] })
    qc.invalidateQueries({ queryKey: ['appts'] })
    qc.invalidateQueries({ queryKey: ['nav'] })
  }
  const setStatus = async (a: Appt, st: ApptStatus) => {
    if (st === 'cancelled' && !confirmDo('Cancel this appointment?')) return
    const r = await act(() => api.post<{ message: string }>(`/appointments/${a.id}/status`, { status: st }))
    if (r) { toast(r.message); refresh() }
  }
  const closeAdd = () => {
    setAdd(false)
    if (sp.get('add') || sp.get('lead')) { sp.delete('add'); sp.delete('lead'); setSp(sp, { replace: true }) }
  }

  if (!sum.data) return <Spinner />
  const s = sum.data
  return <>
    <div className="stats">
      <Kpi label="Today" value={s.kpi.today} ic="clock" c="orange" foot="meetings scheduled" />
      <Kpi label="Upcoming" value={s.kpi.upcoming} ic="cal" c="teal" foot="scheduled from now" />
      <Kpi label="Completed" value={s.kpi.completed} ic="check" c="green" foot={s.kpi.no_show + ' no-shows'} />
      <Kpi label="Self-booked" value={s.kpi.self_booked} ic="link" c="violet" foot="via your booking link" />
    </div>

    <div className="g3r ap">
      <Card title="Next appointments" ic="clock" c="teal" actions={<Btn v="p" sm ic="plus" onClick={() => setAdd(true)}>Book a meeting</Btn>}>
        {!s.upcoming.length && <div className="empty sm"><Icon n="cal" /><h4>No upcoming appointments</h4><p>Share your booking link or book one for a lead.</p></div>}
        {s.upcoming.map(g => <div key={g.label}>
          <div className="ap-g">{g.label}</div>
          {g.rows.map(a => <UpRow key={a.id} a={a} onStatus={setStatus} onEdit={() => setEdit(a)} />)}
        </div>)}
      </Card>
      <div className="col">
        <Card title="Your booking link" ic="link" c="violet">
          <div className="copy"><input readOnly value={s.link} /><Btn sm ic="copy" title="Copy" onClick={() => { copy(s.link); toast('Link copied.') }} /><a className="btn btn-sm" href={s.link} target="_blank" rel="noreferrer" title="Preview"><Icon n="eye" /></a></div>
          <p className="hint">Free slots follow your working hours{s.setup.gcal ? ' and Google Calendar' : ''}. Every lead also has a personal link – use <code>{'{{booking_link}}'}</code> in messages.
            {can('settings') && <> <Link className="lnk" to="/settings/appointments">Hours &amp; reminders</Link></>}</p>
        </Card>
        <Card title="Setup" ic="sliders" c="amber">
          <div className="li"><span className="grow"><b>Hours</b><small>{s.setup.start}–{s.setup.end} · {s.setup.slot} min slots · {s.tz}</small></span></div>
          <div className="li"><span className="grow"><b>Reminders</b><small>{s.setup.remind ? '1 hour before' : 'off'} · {s.setup.notify ? 'email me on booking' : 'no booking email'}</small></span></div>
          <div className="li" style={{ border: 0 }}><span className="grow"><b>Google Calendar</b><small>{s.setup.gcal ? 'Connected' : 'Not connected'}</small></span></div>
        </Card>
      </div>
    </div>

    <Tabs<View> tabs={[{ k: 'calendar', label: 'Calendar' }, { k: 'list', label: 'List' }]} value={view} onChange={k => { k === 'list' ? sp.set('view', 'list') : sp.delete('view'); setSp(sp, { replace: true }) }} />
    {view === 'calendar' ? <Calendar today={s.today} /> : <List onStatus={setStatus} onEdit={setEdit} />}

    {add && <BookModal s={s} pre={pre} onClose={closeAdd} onDone={() => { closeAdd(); refresh() }} />}
    {edit && <EditModal a={edit} onClose={() => setEdit(null)} onDone={() => { setEdit(null); refresh() }} />}
  </>
}

function UpRow({ a, onStatus, onEdit }: { a: Appt; onStatus: (a: Appt, s: ApptStatus) => void; onEdit: () => void }) {
  const t = new Date(a.start_at).getTime()
  const diff = t - Date.now()
  const soon = diff < 3600_000
  const [hh, ap] = a.time.split(' ')
  return <div className={'ap-r' + (soon ? ' soon' : '')}>
    <div className="ap-t"><b>{hh}</b><small>{ap} · {a.mins}m</small></div>
    <div className="grow">
      <b><Link to={'/leads/' + a.lead_id}>{who(a)}</Link>{a.booked_by === 'lead' && <> <Badge c="purple">self-booked</Badge></>}{soon && diff > 0 && <> <Badge c="orange">in {Math.max(1, Math.round(diff / 60000))} min</Badge></>}</b>
      <small>{a.title}{a.user_name && ' · ' + a.user_name}</small>
      <small>{a.phone && <a className="lnk" href={'tel:' + a.phone}><Icon n="phone" c="xs" /> {a.phone}</a>}{a.email && <> · {a.email}</>}{a.location && !a.meet_link && <> · <Icon n="pin" c="xs" /> {a.location.length > 60 ? a.location.slice(0, 59) + '…' : a.location}</>}</small>
    </div>
    <div className="ap-a row">
      {a.meet_link && <a className="btn btn-xs btn-g" href={safeUrl(a.meet_link)} target="_blank" rel="noopener noreferrer"><Icon n="play" />Join</a>}
      <Btn xs ic="check" title="Mark as held" onClick={() => onStatus(a, 'completed')}>Done</Btn>
      <Btn xs onClick={() => onStatus(a, 'no_show')}>No-show</Btn>
      <Btn xs onClick={() => onStatus(a, 'cancelled')}>Cancel</Btn>
      <Btn xs v="ghost" ic="edit" title="Edit" onClick={onEdit} />
    </div>
  </div>
}

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

function Calendar({ today }: { today: string }) {
  const [m, setM] = useState(today.slice(0, 7))
  const first = m + '-01'
  const [y, mo] = m.split('-').map(Number)
  const dow = (new Date(Date.UTC(y, mo - 1, 1)).getUTCDay() + 6) % 7 // Monday = 0
  const start = addDays(first, -dow)
  const end = addDays(start, 41)
  const q = useQuery({ queryKey: ['appts', 'cal', start], queryFn: () => api.get<ApptPage>('/appointments', { from: start, to: end, per: 1000 }) })
  const by = useMemo(() => {
    const o: Record<string, Appt[]> = {}
    q.data?.rows.forEach(a => { (o[a.day] ||= []).push(a) })
    return o
  }, [q.data])
  const shift = (n: number) => { const d = new Date(Date.UTC(y, mo - 1 + n, 1)); setM(d.toISOString().slice(0, 7)) }
  const days = Array.from({ length: 42 }, (_, i) => addDays(start, i))
  return <Card title={MONTHS[mo - 1] + ' ' + y} ic="cal" c="indigo" actions={<>
    <button type="button" className="ib bd" title="Previous month" onClick={() => shift(-1)}>‹</button>
    <Btn sm onClick={() => setM(today.slice(0, 7))}>Today</Btn>
    <button type="button" className="ib bd" title="Next month" onClick={() => shift(1)}>›</button>
  </>}>
    <div className="cal">
      {['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'].map(d => <div key={d} className="ch">{d}</div>)}
      {days.map(d => <div key={d} className={[d.slice(0, 7) !== m && 'out', d === today && 'td'].filter(Boolean).join(' ')}>
        <span className="dn">{Number(d.slice(8))}</span>
        {(by[d] || []).map(a => <Link key={a.id} className={'ev' + (a.status === 'completed' ? ' done' : a.status === 'cancelled' || a.status === 'no_show' ? ' cx' : '')} to={'/leads/' + a.lead_id} title={a.title}>{a.local.slice(11)} {who(a)}</Link>)}
      </div>)}
    </div>
  </Card>
}

function List({ onStatus, onEdit }: { onStatus: (a: Appt, s: ApptStatus) => void; onEdit: (a: Appt) => void }) {
  const can = useCan()
  const [f, setF] = useState({ q: '', status: 'upcoming', by: '', user: '', from: '', to: '', page: 1 })
  const set = (k: string, v: string | number) => setF(x => ({ ...x, [k]: v, page: k === 'page' ? Number(v) : 1 }))
  const q = useQuery({ queryKey: ['appts', 'list', f], queryFn: () => api.get<ApptPage>('/appointments', { ...f, sort: f.status === 'upcoming' || f.status === 'scheduled' ? '' : 'desc' }) })
  const team = useQuery({ queryKey: ['team'], queryFn: () => api.get<{ members: { id: number; name: string; active: boolean }[] }>('/team'), enabled: can('team') })
  const dirty = f.q || f.status !== 'upcoming' || f.by || f.user || f.from || f.to
  return <>
    <div className="fbar">
      <label className="srch"><Icon n="search" /><input placeholder="Search name, email, phone, title…" value={f.q} onChange={e => set('q', e.target.value)} /></label>
      <select value={f.status} className={f.status !== 'upcoming' ? 'on' : ''} onChange={e => set('status', e.target.value)}>
        <option value="upcoming">Upcoming</option><option value="">All appointments</option><option value="past">Past</option>
        {Object.entries(STATUS).map(([k, [l]]) => <option key={k} value={k}>{l}</option>)}
      </select>
      <select value={f.by} className={f.by ? 'on' : ''} onChange={e => set('by', e.target.value)}><option value="">Booked by anyone</option><option value="lead">Self-booked by the lead</option><option value="admin">Booked by the team</option></select>
      {team.data && <select value={f.user} className={f.user ? 'on' : ''} onChange={e => set('user', e.target.value)}><option value="">All owners</option>{team.data.members.filter(u => u.active).map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select>}
      <input type="date" title="From" value={f.from} className={f.from ? 'on' : ''} onChange={e => set('from', e.target.value)} />
      <input type="date" title="To" value={f.to} className={f.to ? 'on' : ''} onChange={e => set('to', e.target.value)} />
      {dirty && <a href="#" className="clr-f" onClick={e => { e.preventDefault(); setF({ q: '', status: 'upcoming', by: '', user: '', from: '', to: '', page: 1 }) }}>Clear</a>}
    </div>
    <Card p0>
      {!q.data ? <Spinner /> : !q.data.rows.length ? <Empty ic="cal" title="No appointments">Nothing matches these filters.</Empty> :
        <div className="tw"><table className="tbl"><thead><tr><th>When</th><th>Lead</th><th>Title</th><th>Owner</th><th>Status</th><th></th></tr></thead>
          <tbody>{q.data.rows.map(a => <tr key={a.id}>
            <td><b>{new Date(a.day + 'T00:00:00').toLocaleDateString('en-GB', { weekday: 'short', day: '2-digit', month: 'short', year: 'numeric' })}</b><br /><small className="muted">{a.time} · {a.mins} min</small></td>
            <td><Link className="lnk" to={'/leads/' + a.lead_id}>{who(a)}</Link>{a.booked_by === 'lead' && <> <Badge c="purple">self-booked</Badge></>}<br /><small className="muted">{[a.phone, a.email].filter(Boolean).join(' · ')}</small></td>
            <td>{a.title}{a.meet_link ? <><br /><a className="lnk xs" href={safeUrl(a.meet_link)} target="_blank" rel="noopener noreferrer">Google Meet</a></> : a.location && <><br /><small className="muted">{a.location}</small></>}</td>
            <td>{a.user_name || '—'}</td>
            <td><Badge c={STATUS[a.status][1]}>{STATUS[a.status][0]}</Badge></td>
            <td className="row nw">
              {a.status === 'scheduled' && <><Btn xs ic="check" onClick={() => onStatus(a, 'completed')}>Done</Btn><Btn xs onClick={() => onStatus(a, 'no_show')}>No-show</Btn><Btn xs onClick={() => onStatus(a, 'cancelled')}>Cancel</Btn></>}
              {a.status !== 'scheduled' && <Btn xs ic="refresh" onClick={() => onStatus(a, 'scheduled')}>Reopen</Btn>}
              <Btn xs v="ghost" ic="edit" title="Edit" onClick={() => onEdit(a)} />
            </td>
          </tr>)}</tbody></table></div>}
      {q.data && <Pager page={q.data.page} pages={q.data.pages} total={q.data.total} label="appointments" onPage={p => set('page', p)} />}
    </Card>
  </>
}

interface LeadOpt { id: number; name: string; contact_name: string }

function BookModal({ s, pre, onClose, onDone }: { s: Summary; pre: number; onClose: () => void; onDone: () => void }) {
  const me = useMe(), can = useCan()
  const leads = useQuery({ queryKey: ['appt-leads'], queryFn: () => api.get<LeadOpt[]>('/appointments/leads') })
  const team = useQuery({ queryKey: ['team'], queryFn: () => api.get<{ members: { id: number; name: string; active: boolean }[] }>('/team'), enabled: can('team') })
  const [v, setV] = useState<Record<string, string>>({ lead_id: pre ? String(pre) : '', user_id: String(me.user.id) })
  const [busy, setBusy] = useState(false)
  const set = (k: string, x: string) => setV(o => ({ ...o, [k]: x }))
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api.post<{ message: string }>('/appointments', { ...v, lead_id: Number(v.lead_id || 0), user_id: Number(v.user_id || 0) })
      toast(r.message)
      onDone()
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  return <Modal open onClose={onClose} title="Book a meeting" ic="plus" c="teal">
    <form onSubmit={submit}>
      <div className="fg">
        <Field label="Lead"><select value={v.lead_id} onChange={e => set('lead_id', e.target.value)}>
          <option value="">— New person —</option>
          {leads.data?.map(l => <option key={l.id} value={l.id}>{l.contact_name || l.name}</option>)}
        </select></Field>
        {!v.lead_id && <>
          <Field label="Name"><input value={v.name || ''} onChange={e => set('name', e.target.value)} /></Field>
          <Field label="Phone"><input type="tel" value={v.phone || ''} onChange={e => set('phone', e.target.value)} /></Field>
          <Field label="Email"><input type="email" value={v.email || ''} onChange={e => set('email', e.target.value)} /></Field>
          <Field label="Business"><input value={v.company || ''} onChange={e => set('company', e.target.value)} /></Field>
        </>}
        <Field label="Date & time" hint={'Workspace time zone: ' + s.tz}><input type="datetime-local" required min={s.now} value={v.at || ''} onChange={e => set('at', e.target.value)} /></Field>
        <Field label="Title"><input placeholder={s.setup.title} value={v.title || ''} onChange={e => set('title', e.target.value)} /></Field>
        {team.data && <Field label="Owner"><select value={v.user_id} onChange={e => set('user_id', e.target.value)}>{team.data.members.filter(u => u.active).map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select></Field>}
        <Field label="Notes"><textarea rows={2} value={v.notes || ''} onChange={e => set('notes', e.target.value)} /></Field>
      </div>
      <div className="md-f"><span className="muted xs grow">{s.setup.slot} min{s.setup.gcal ? ' · added to Google Calendar' : ''} · confirmation by email</span><Btn onClick={onClose}>Cancel</Btn><Btn v="p" type="submit" ic="check" busy={busy}>Book meeting</Btn></div>
    </form>
  </Modal>
}

function EditModal({ a, onClose, onDone }: { a: Appt; onClose: () => void; onDone: () => void }) {
  const can = useCan()
  const team = useQuery({ queryKey: ['team'], queryFn: () => api.get<{ members: { id: number; name: string; active: boolean }[] }>('/team'), enabled: can('team') })
  const [v, setV] = useState({ at: a.local, minutes: String(a.mins), title: a.title, location: a.location, notes: a.notes, status: a.status as string, user_id: String(a.user_id || '') })
  const [busy, setBusy] = useState(false)
  const set = (k: string, x: string) => setV(o => ({ ...o, [k]: x }))
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body: Record<string, unknown> = { title: v.title, location: v.location, notes: v.notes, status: v.status, minutes: Number(v.minutes) || 0 }
      if (v.at !== a.local) body.at = v.at
      if (v.user_id) body.user_id = Number(v.user_id)
      await api.put('/appointments/' + a.id, body)
      toast('Appointment updated.')
      onDone()
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  return <Modal open onClose={onClose} title={'Edit · ' + who(a)} ic="edit" c="teal">
    <form onSubmit={submit}>
      {a.in_gcal && <Alert>Changes are also made in Google Calendar; cancelling removes the event and notifies the guest.</Alert>}
      <div className="fg">
        <Field label="Date & time"><input type="datetime-local" required value={v.at} onChange={e => set('at', e.target.value)} /></Field>
        <Field label="Length (minutes)"><input type="number" min={5} max={1440} value={v.minutes} onChange={e => set('minutes', e.target.value)} /></Field>
        <Field label="Title"><input value={v.title} onChange={e => set('title', e.target.value)} /></Field>
        <Field label="Location / meeting link"><input value={v.location} onChange={e => set('location', e.target.value)} /></Field>
        <Field label="Status"><select value={v.status} onChange={e => set('status', e.target.value)}>{Object.entries(STATUS).map(([k, [l]]) => <option key={k} value={k}>{l}</option>)}</select></Field>
        {team.data && <Field label="Owner"><select value={v.user_id} onChange={e => set('user_id', e.target.value)}><option value="">—</option>{team.data.members.filter(u => u.active).map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select></Field>}
        <Field label="Notes"><textarea rows={3} value={v.notes} onChange={e => set('notes', e.target.value)} /></Field>
      </div>
      <div className="md-f"><Link className="btn" to={'/leads/' + a.lead_id}><Icon n="user" />Open lead</Link><span className="grow" /><Btn onClick={onClose}>Close</Btn><Btn v="p" type="submit" ic="check" busy={busy}>Save</Btn></div>
    </form>
  </Modal>
}
