// Dashboard: greeting, KPI cards with trend pills, leads-over-time area chart, pipeline donut,
// recent activity, agenda (meetings & follow-ups), lead sources, prospecting funnel and setup checklist.
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe } from '@/lib/session'
import { ago, money, num } from '@/lib/format'
import { AreaChart, Avatar, Donut, EmptyState, Hi, Icon, Point, Progress, Skeleton, Sparkline, Trend, usePageTitle } from '@/components/ui'
import { CalendarJuggler, CoffeeBot, RocketRider, SleepyCat, Trophy } from '@/components/Cartoons'
import { CC, EVIC, SOURCES, STATUSES, telHref } from './common'

interface Dash {
  kpi: { total: number; today: number; week: number; new: number; won: number; won_value: number; pipeline: number; due: number }
  status: { k: string; n: number }[]; sources: { k: string; n: number }[]
  appts: { id: number; lead_id: number; title: string; start_at: string; meet_link: string; name: string; contact_name: string }[]
  upcoming: number; follow_ups: { id: number; name: string; contact_name: string; follow_up_at: string; phone: string }[]
  events: { id: number; lead_id: number; type: string; message: string; created_at: string; name: string; contact_name: string }[]
  pro: boolean; prospecting?: Record<string, number>; checks?: { ok: boolean; label: string; to: string }[]
  /** optional: leads per day [{d: 'YYYY-MM-DD', n}] – used when the server provides it */
  series?: { d: string; n: number }[]
}

const DAYS = 14
const dd = (ts: string) => String(new Date(ts).getDate()).padStart(2, '0')
const mon = (ts: string) => new Date(ts).toLocaleString('en-GB', { month: 'short' })
const tm = (ts: string) => new Date(ts).toLocaleString('en-US', { hour: '2-digit', minute: '2-digit' })
const dayTm = (ts: string) => new Date(ts).toLocaleString('en-US', { weekday: 'short' }) + ' ' + tm(ts)
const key = (d: Date) => d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0')

/** Leads per day for the last DAYS days: from the server series, else from the newest 100 leads. */
function useSeries(d?: Dash) {
  const rows = useQuery({
    queryKey: ['dash-series'], enabled: !!d && !d.series, staleTime: 60_000,
    queryFn: () => api.get<{ rows: { created_at: string }[] }>('/leads', { per: 100, stats: 0 }).then(r => r.rows || []).catch(() => [] as { created_at: string }[]),
  })
  if (!d) return null
  const start = new Date(); start.setHours(0, 0, 0, 0); start.setDate(start.getDate() - (DAYS - 1))
  const count: Record<string, number> = {}
  let from = start
  if (d.series) d.series.forEach(x => { count[x.d.slice(0, 10)] = x.n })
  else {
    if (!rows.data) return null
    rows.data.forEach(r => { const k = key(new Date(r.created_at)); count[k] = (count[k] || 0) + 1 })
    // only 100 leads were read: start at the first day they fully cover
    if (rows.data.length >= 100) { const o = new Date(rows.data[rows.data.length - 1].created_at); o.setHours(0, 0, 0, 0); o.setDate(o.getDate() + 1); if (o > from) from = o }
  }
  const pts: Point[] = []
  for (const t = new Date(from); t <= new Date(); t.setDate(t.getDate() + 1)) {
    pts.push({ label: t.toLocaleDateString('en-GB', { day: '2-digit', month: 'short' }), value: count[key(t)] || 0, hint: t.toLocaleDateString('en-GB', { weekday: 'long', day: '2-digit', month: 'short' }) })
  }
  return pts
}

function Loading() {
  return <>
    <div className="dh"><div><Skeleton h={30} w={320} /><Skeleton h={14} w={220} className="mt" /></div></div>
    <div className="kpis">{[0, 1, 2, 3].map(i => <div key={i} className="kpi"><Skeleton h={40} w={40} r={40} /><Skeleton h={28} w="60%" className="mt" /><Skeleton h={12} w="80%" className="mt" /></div>)}</div>
    <div className="dg eq"><div className="card"><Skeleton h={260} /></div><div className="card"><Skeleton h={260} /></div></div>
  </>
}

export default function Dashboard() {
  const me = useMe()
  const now = new Date()
  usePageTitle('Dashboard', now.toLocaleDateString('en-GB', { weekday: 'long', day: '2-digit', month: 'long', year: 'numeric' }))
  const q = useQuery({ queryKey: ['dashboard'], queryFn: () => api.get<Dash>('/dashboard'), refetchInterval: 60_000 })
  const d = q.data
  const series = useSeries(d)
  if (!d) return <Loading />
  const sym = me.settings.currency_symbol || '₹'
  const k = d.kpi
  const h = now.getHours()
  const hi = h < 12 ? 'Good morning' : h < 17 ? 'Good afternoon' : 'Good evening'
  const st: Record<string, number> = Object.fromEntries(d.status.map(x => [x.k, x.n]))
  const checks = d.checks || []
  const done = checks.filter(c => c.ok).length
  const p = d.prospecting
  const before = k.total - k.week
  const growth = before > 0 ? Math.round((k.week / before) * 100) : k.week > 0 ? 100 : 0
  const open = k.total - (st.won || 0) - (st.lost || 0)
  const winRate = (st.won || 0) + (st.lost || 0) > 0 ? Math.round(((st.won || 0) / ((st.won || 0) + (st.lost || 0))) * 100) : 0
  const inRange = series ? series.reduce((a, b) => a + b.value, 0) : 0
  const half = series ? Math.floor(series.length / 2) : 0
  const recent = series ? series.slice(half).reduce((a, b) => a + b.value, 0) : 0
  const prior = series ? series.slice(0, half).reduce((a, b) => a + b.value, 0) : 0
  const trend = prior > 0 ? Math.round(((recent - prior) / prior) * 100) : recent > 0 ? 100 : 0
  const srcMax = Math.max(1, ...d.sources.map(s => s.n))
  const ringC = 2 * Math.PI * 33

  const spark = series ? series.map(x => x.value) : []
  const cum = spark.reduce<number[]>((a, v) => { a.push((a[a.length - 1] ?? Math.max(0, k.total - inRange)) + v); return a }, [])
  return <>
    <div className="greet">
      <div className="greet-c">{k.won > 0 && k.due === 0 ? <Trophy w={116} /> : <RocketRider w={116} />}
      <div className="dh"><div><h2>{hi}, <span>{me.user.name.split(' ')[0]}</span></h2>
        <p>{k.due ? <Link to="/leads?fu=due" className="chipx hot"><Icon n="flame" />{k.due} follow-up{k.due > 1 ? 's' : ''} due</Link> : <span className="chipx c-green"><Icon n="check" />No follow-ups due</span>}
          {d.appts.length > 0 && <Link to="/appointments" className="chipx c-teal"><Icon n="cal" />Next meeting {dayTm(d.appts[0].start_at)}</Link>}
          {k.today > 0 && <span className="chipx c-indigo"><Icon n="zap" />{k.today} new today</span>}</p></div></div></div>
      <div className="pha"><Link className="btn" to="/appointments?add=1"><Icon n="cal" />Book meeting</Link>{d.pro && <Link className="btn" to="/scraper?new=1"><Icon n="map" />Map search</Link>}<Link className="btn btn-p" to="/leads/new"><Icon n="plus" />Add lead</Link></div>
    </div>

    <div className="kpis">
      <Link to="/leads" className="kpi hot"><div className="kpi-h"><span className="kpi-l">{me.user.role === 'sales' ? 'My leads' : 'Total leads'}</span><Hi n="users" c="indigo" /></div>
        <div className="kpi-m"><b className="kpi-v">{num(k.total)}</b>{cum.length > 1 && <Sparkline data={cum} w={92} h={34} />}</div><span className="kpi-f"><Trend v={growth} label="this week" /></span></Link>
      <Link to="/leads?status=new" className="kpi c-sky"><div className="kpi-h"><span className="kpi-l">New – need first touch</span><Hi n="zap" c="sky" /></div>
        <div className="kpi-m"><b className="kpi-v">{num(k.new)}</b>{spark.length > 1 && <Sparkline data={spark} w={92} h={34} />}</div><span className="kpi-f"><span className="trend flat">+{k.week} this week</span></span></Link>
      <Link to="/leads?fu=due" className="kpi c-orange"><div className="kpi-h"><span className="kpi-l">Follow-ups due</span><Hi n="clock" c="orange" /></div>
        <div className="kpi-m"><b className="kpi-v">{num(k.due)}</b></div><span className="kpi-f">{k.due ? <span className="trend dn">Act today</span> : <span className="trend up"><Icon n="check" />All caught up</span>}</span></Link>
      <Link to="/leads?status=won" className="kpi tone-ok"><div className="kpi-h"><span className="kpi-l">Won</span><Hi n="target" c="green" /></div>
        <div className="kpi-m"><b className="kpi-v">{money(k.won_value, sym)}</b></div><span className="kpi-f"><span className="trend up">{k.won} deal{k.won === 1 ? '' : 's'}</span>{money(k.pipeline, sym)} open</span></Link>
    </div>

    <div className="dg">
      <div className="card">
        <div className="ch-h"><div><h3><Hi n="activity" c="indigo" />Leads over time</h3>
          <div className="ch-v"><b>{num(inRange)}</b><span>new leads · last {series?.length || DAYS} days</span>{series && series.length > 3 && <Trend v={trend} label="vs previous" />}</div></div>
          <Link className="lnk sm" to="/leads">All leads <Icon n="arrowR" /></Link></div>
        {!series ? <Skeleton h={220} /> : inRange === 0 ? <EmptyState art={<RocketRider />} title="No new leads yet" action={<Link className="btn btn-p btn-sm" to="/leads/new"><Icon n="plus" />Add lead</Link>}>New leads from forms, ads and your team show up here day by day.</EmptyState>
          : <AreaChart data={series} height={230} />}
      </div>
      <div className="card">
        <div className="ch-h"><h3><Hi n="kanban" c="green" />Pipeline</h3><Link className="lnk sm" to="/leads?view=board">Board <Icon n="arrowR" /></Link></div>
        <Donut size={164} thick={20} center={<><b>{num(open)}</b><small>open leads</small></>}
          data={STATUSES.map(s => ({ label: s.label, value: st[s.key] || 0, c: CC[s.color], to: '/leads?status=' + s.key }))} />
        <div className="row mt between sm muted" style={{ borderTop: '1px solid var(--line2)', paddingTop: 12 }}><span>Win rate <b style={{ color: 'var(--ink)' }}>{winRate}%</b></span><span>Pipeline <b style={{ color: 'var(--ink)' }}>{money(k.pipeline, sym)}</b></span></div>
      </div>
    </div>

    <div className="dg eq">
      <div className="card">
        <div className="ch-h"><h3><Hi n="bell" c="violet" />Recent activity</h3><Link className="lnk sm" to="/activity">View all <Icon n="arrowR" /></Link></div>
        {!d.events.length ? <EmptyState art={<SleepyCat />} title="All quiet for now">Add a lead to get started – calls, emails and status changes appear here.</EmptyState> :
          <ul className="feed2">{d.events.map(x => { const nm = x.contact_name || x.name; return <li key={x.id}>
            <Avatar name={nm} size={36} />
            <div className="fe-b"><Link to={'/leads/' + x.lead_id}><b>{nm}</b></Link><p>{x.message}</p></div>
            <span className={'fe-i ev-' + x.type} title={x.type}><Icon n={EVIC[x.type] || 'zap'} /></span>
            <small>{ago(x.created_at)}</small></li> })}</ul>}
      </div>
      <div className="col">
        <div className="card agl dk c-teal">
          <div className="ch-h"><h3><Hi n="cal" c="teal" />Today &amp; next</h3><Link className="lnk sm" to="/appointments">Calendar <Icon n="arrowR" /></Link></div>
          {!d.appts.length && !d.follow_ups.length && <div className="empty sm agl-e"><CalendarJuggler w={150} /><p>Nothing in the next 24 hours – enjoy the calm.</p></div>}
          {d.appts.slice(0, 3).map(a => <div key={'a' + a.id} className="li"><span className="when c-teal"><b>{dd(a.start_at)}</b><small>{mon(a.start_at)}</small></span>
            <div className="grow"><b>{a.lead_id ? <Link to={'/leads/' + a.lead_id}>{a.contact_name || a.name}</Link> : a.title}</b><small>{dayTm(a.start_at)} · {a.title}</small></div>
            {a.meet_link ? <a className="btn btn-xs btn-g" href={a.meet_link} target="_blank" rel="noopener noreferrer">Join</a> : <span className="bdg c-teal">Meeting</span>}</div>)}
          {d.follow_ups.map(f => { const late = new Date(f.follow_up_at).getTime() <= Date.now(); return <div key={'f' + f.id} className="li"><span className={'when ' + (late ? 'c-rose' : 'c-orange')}><b>{dd(f.follow_up_at)}</b><small>{mon(f.follow_up_at)}</small></span>
            <div className="grow"><b><Link to={'/leads/' + f.id}>{f.contact_name || f.name}</Link></b><small>{tm(f.follow_up_at)}{f.phone && ' · ' + f.phone}</small></div>
            {f.phone && <a className="ib bd" href={telHref(f.phone)} title="Call" aria-label={'Call ' + (f.contact_name || f.name)}><Icon n="phone" /></a>}<span className={'bdg bdg-dot ' + (late ? 'c-rose' : 'c-orange')}>{late ? 'Overdue' : 'Due'}</span></div> })}
          {k.due > d.follow_ups.length && <div className="vm"><Link to="/leads?fu=due">View all {k.due} follow-ups →</Link></div>}
        </div>
        {d.sources.length > 0 && <div className="card">
          <div className="ch-h"><h3><Hi n="plug" c="pink" />Lead sources</h3></div>
          <div className="col" style={{ gap: 12 }}>{d.sources.slice(0, 5).map(s => { const [lb, c] = SOURCES[s.k] || [s.k, 'gray']; return <Link key={s.k} to={'/leads?src=' + s.k}><Progress value={s.n} max={srcMax} c={CC[c] || c} label={lb} sub={num(s.n)} /></Link> })}</div>
        </div>}
        {d.pro && p && (() => {
          const fun: [string, number, string][] = [['Scraped', p.t, 'gray'], ['Audited', p.au, 'sky'], ['Report ready', p.rr, 'violet'], ['Contacted', p.ct, 'amber'], ['Replied', p.rp, 'green'], ['Moved to Leads', p.il, 'pink']]
          const max = Math.max(1, p.t)
          return <div className="card"><div className="ch-h"><h3><Hi n="map" c="orange" />Prospecting</h3><Link className="lnk sm" to="/prospects">Scraped data <Icon n="arrowR" /></Link></div>
            <div className="bars">{fun.map(([lb, n, c]) => <div key={lb} className={'bar c-' + c}><span>{lb}</span><div><i style={{ width: Math.max(2, Math.round(n / max * 100)) + '%' }} /></div><b>{n}</b></div>)}</div>
            {p.wk > 0 && <p className="hint"><span className="spin" /> {p.wk} processing now</p>}</div>
        })()}
      </div>
    </div>

    {checks.length > 0 && done < checks.length && <div className="card">
      <div className="setup">
        <CoffeeBot w={110} className="setup-art" />
        <div className="ring"><svg width="74" height="74" viewBox="0 0 74 74"><circle className="r0" cx="37" cy="37" r="33" /><circle className="r1" cx="37" cy="37" r="33" strokeDasharray={`${(done / checks.length) * ringC} ${ringC}`} /></svg><b>{Math.round((done / checks.length) * 100)}%</b></div>
        <div className="grow"><h3>Finish setting up</h3><p className="muted sm mb0">{done} of {checks.length} done – a few minutes here pays off every day.</p></div>
      </div>
      <div className="steps mt">{checks.filter(c => !c.ok).map(c => <Link key={c.label} to={c.to} className="step"><Icon n="clock" /><span>{c.label}</span></Link>)}</div>
    </div>}
  </>
}
