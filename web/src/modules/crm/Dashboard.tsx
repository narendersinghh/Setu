// Dashboard (v1 dashboard.php): KPIs, agenda, pipeline bars, activity, prospecting funnel, sources, setup checklist.
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe } from '@/lib/session'
import { ago, money, num } from '@/lib/format'
import { Hi, Icon, Kpi, Spinner, usePageTitle } from '@/components/ui'
import { CC, EVIC, SOURCES, STATUSES, telHref } from './common'

interface Dash {
  kpi: { total: number; today: number; week: number; new: number; won: number; won_value: number; pipeline: number; due: number }
  status: { k: string; n: number }[]; sources: { k: string; n: number }[]
  appts: { id: number; lead_id: number; title: string; start_at: string; meet_link: string; name: string; contact_name: string }[]
  upcoming: number; follow_ups: { id: number; name: string; contact_name: string; follow_up_at: string; phone: string }[]
  events: { id: number; lead_id: number; type: string; message: string; created_at: string; name: string; contact_name: string }[]
  pro: boolean; prospecting?: Record<string, number>; checks?: { ok: boolean; label: string; to: string }[]
}

const dd = (ts: string) => String(new Date(ts).getDate()).padStart(2, '0')
const mon = (ts: string) => new Date(ts).toLocaleString('en-GB', { month: 'short' })
const tm = (ts: string) => new Date(ts).toLocaleString('en-US', { hour: '2-digit', minute: '2-digit' })
const dayTm = (ts: string) => new Date(ts).toLocaleString('en-US', { weekday: 'short' }) + ' ' + tm(ts)

export default function Dashboard() {
  const me = useMe()
  const now = new Date()
  usePageTitle('Dashboard', now.toLocaleDateString('en-GB', { weekday: 'long', day: '2-digit', month: 'long', year: 'numeric' }))
  const q = useQuery({ queryKey: ['dashboard'], queryFn: () => api.get<Dash>('/dashboard'), refetchInterval: 60_000 })
  const d = q.data
  if (!d) return <Spinner />
  const sym = me.settings.currency_symbol || '₹'
  const k = d.kpi
  const h = now.getHours()
  const hi = h < 12 ? 'Good morning' : h < 17 ? 'Good afternoon' : 'Good evening'
  const st: Record<string, number> = Object.fromEntries(d.status.map(x => [x.k, x.n]))
  const mx = Math.max(1, ...Object.values(st))
  const agenda = d.appts.length + d.follow_ups.length
  const checks = d.checks || []
  const done = checks.filter(c => c.ok).length
  const p = d.prospecting
  return <>
    <div className="ph"><div><h2 style={{ fontSize: 20 }}>{hi}, {me.user.name.split(' ')[0]} 👋</h2>
      <p className="muted sm mb0">{k.due ? <b className="red">{k.due} follow-up{k.due > 1 ? 's' : ''} due</b> : 'No follow-ups due'}{d.appts.length > 0 && ' · next meeting ' + dayTm(d.appts[0].start_at)}</p></div>
      <div className="pha"><Link className="btn btn-sm" to="/appointments?add=1"><Icon n="cal" />Book meeting</Link>{d.pro && <Link className="btn btn-sm" to="/scraper?new=1"><Icon n="map" />Map search</Link>}<Link className="btn btn-sm btn-p" to="/leads/new"><Icon n="plus" />Add lead</Link></div></div>
    <div className="stats">
      <Kpi label="Follow-ups due" value={k.due} ic="clock" c="orange" foot={k.due ? <span className="dn">act today</span> : 'all caught up'} to="/leads?fu=due" />
      <Kpi label="New – need first touch" value={k.new} ic="zap" c="sky" foot="waiting for a call or message" to="/leads?status=new" />
      <Kpi label="Upcoming meetings" value={d.upcoming} ic="cal" c="teal" foot={d.appts.length ? 'next ' + new Date(d.appts[0].start_at).toLocaleString('en-GB', { day: '2-digit', month: 'short' }) + ', ' + tm(d.appts[0].start_at) : 'none booked'} to="/appointments" />
      <Kpi label={me.user.role === 'sales' ? 'My leads' : 'Total leads'} value={num(k.total)} ic="users" c="indigo" foot={<><span className="up">+{k.week}</span> this week · {k.today} today</>} to="/leads" />
      <Kpi label="Won" value={money(k.won_value, sym)} ic="target" c="green" foot={k.won + ' deals · ' + money(k.pipeline, sym) + ' open'} to="/leads?status=won" />
    </div>
    <div className="g2 compact">
      <div className="card"><div className="card-h"><h3><Hi n="cal" c="teal" />Today &amp; next</h3><Link className="lnk sm" to="/appointments">Calendar</Link></div>
        {!agenda && <div className="empty sm"><Icon n="cal" /><p>Nothing in the next 24 hours.</p></div>}
        {d.appts.slice(0, 3).map(a => <div key={'a' + a.id} className="li"><span className="when c-teal"><b>{dd(a.start_at)}</b><small>{mon(a.start_at)}</small></span>
          <div className="grow"><b>{a.lead_id ? <Link to={'/leads/' + a.lead_id}>{a.contact_name || a.name}</Link> : a.title}</b><small>{dayTm(a.start_at)} · {a.title}</small></div>
          {a.meet_link ? <a className="btn btn-xs btn-g" href={a.meet_link} target="_blank" rel="noopener noreferrer">Join</a> : <span className="bdg c-teal">Meeting</span>}</div>)}
        {d.follow_ups.map(f => { const late = new Date(f.follow_up_at).getTime() <= Date.now(); return <div key={'f' + f.id} className="li"><span className={'when ' + (late ? 'c-rose' : 'c-orange')}><b>{dd(f.follow_up_at)}</b><small>{mon(f.follow_up_at)}</small></span>
          <div className="grow"><b><Link to={'/leads/' + f.id}>{f.contact_name || f.name}</Link></b><small>{tm(f.follow_up_at)}{f.phone && ' · ' + f.phone}</small></div>
          {f.phone && <a className="ib bd" href={telHref(f.phone)} title="Call"><Icon n="phone" /></a>}<span className={'bdg ' + (late ? 'c-rose' : 'c-orange')}>{late ? 'Overdue' : 'Due'}</span></div> })}
        {k.due > d.follow_ups.length && <div className="vm"><Link to="/leads?fu=due">View all {k.due} follow-ups →</Link></div>}
      </div>
      <div className="card"><div className="card-h"><h3><Hi n="kanban" c="green" />Pipeline</h3><Link className="lnk sm" to="/leads">All leads</Link></div>
        <div className="bars">{STATUSES.map(s => { const n = st[s.key] || 0; return <Link key={s.key} className={'bar c-' + CC[s.color]} to={'/leads?status=' + s.key}><span>{s.label}</span><div><i style={{ width: Math.max(2, Math.round(n / mx * 100)) + '%' }} /></div><b>{n}</b></Link> })}</div></div>
    </div>
    <div className="g2 compact">
      <div className="card"><div className="card-h"><h3><Hi n="activity" c="violet" />Recent activity</h3></div>
        {!d.events.length ? <div className="empty sm"><Icon n="activity" /><p>No activity yet. Add a lead to get started.</p></div> : <>
          <ul className="tl" style={{ marginTop: 0 }}>{d.events.map(x => <li key={x.id} className={'ev-' + x.type}><i><Icon n={EVIC[x.type] || 'zap'} /></i><div><span><Link to={'/leads/' + x.lead_id}><b>{x.contact_name || x.name}</b></Link> · {x.message.length > 90 ? x.message.slice(0, 89) + '…' : x.message}</span><small>{ago(x.created_at)}</small></div></li>)}</ul>
          <div className="vm"><Link to="/activity">View all activity →</Link></div></>}
      </div>
      <div className="col">
        {d.pro && p && (() => {
          const fun: [string, number, string][] = [['Scraped', p.t, 'gray'], ['Audited', p.au, 'sky'], ['Report ready', p.rr, 'violet'], ['Contacted', p.ct, 'amber'], ['Replied', p.rp, 'green'], ['Moved to Leads', p.il, 'pink']]
          const max = Math.max(1, p.t)
          return <div className="card"><div className="card-h"><h3><Hi n="map" c="orange" />Prospecting</h3><Link className="lnk sm" to="/prospects">Scraped data</Link></div>
            <div className="bars">{fun.map(([lb, n, c]) => <div key={lb} className={'bar c-' + c}><span>{lb}</span><div><i style={{ width: Math.max(2, Math.round(n / max * 100)) + '%' }} /></div><b>{n}</b></div>)}</div>
            {p.wk > 0 && <p className="hint"><span className="spin" /> {p.wk} processing now</p>}</div>
        })()}
        {d.sources.length > 0 && <div className="card"><div className="card-h"><h3><Hi n="plug" c="pink" />Lead sources</h3></div>
          <div className="chips mb0">{d.sources.map(s => { const [lb, c, ic] = SOURCES[s.k] || [s.k, 'gray', 'link']; return <Link key={s.k} to={'/leads?src=' + s.k} className={'src-' + c}><Icon n={ic} />{lb} <em>{s.n}</em></Link> })}</div></div>}
      </div>
    </div>
    {checks.length > 0 && done < checks.length && <div className="card compact"><div className="card-h"><h3><Hi n="zap" c="amber" />Finish setting up</h3><span className="bdg c-amber">{done} of {checks.length} done</span></div>
      <div className="steps">{checks.filter(c => !c.ok).map(c => <Link key={c.label} to={c.to} className="step"><Icon n="clock" /><span>{c.label}</span></Link>)}</div></div>}
  </>
}
