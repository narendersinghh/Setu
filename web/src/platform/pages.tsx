// Platform console pages: overview, organisations (+ detail), plans (cards + editor), payments (gateways + all
// payments), branding (live preview), email, updates, platform admins. Styles: platform.css (pc-*) + the app kit.
import { CSSProperties, FormEvent, ReactNode, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ago, date, dateTime, num } from '@/lib/format'
import { BrandName, inkOn, useBrand, useTheme } from '@/lib/theme'
import { Alert, Avatar, Badge, BarChart, Btn, Card, ChipsInput, Donut, EmptyState, Field, Hi, Icon, Kpi, Modal, Progress, Secret, Sec, Skeleton, Spinner, Switch, act, confirmDo, toast, usePageTitle } from '@/components/ui'
import { CoffeeBot, Detective, PiggyBank, RocketRider, SleepyCat } from '@/components/Cartoons'

type LimitKey = [string, string]
const SYM: Record<string, string> = { INR: '₹', USD: '$', EUR: '€', GBP: '£', AED: 'AED ', AUD: 'A$', CAD: 'C$', SGD: 'S$' }
const sym = (c?: string) => (c ? SYM[c] ?? c + ' ' : '₹')
const price = (n: number, cur: string) => (n ? sym(cur) + num(n) : 'Free')
const dateInput = (v?: string | null) => (v ? v.slice(0, 10) : '')
const toISO = (v: string) => (v ? new Date(v + 'T23:59:59').toISOString() : null)
const future = (v?: string | null) => !!v && new Date(v).getTime() > Date.now()
const statusBadge = (s: string) => s === 'active' ? <Badge c="green" dot>Active</Badge> : s === 'suspended' ? <Badge c="red" dot>Suspended</Badge> : <Badge>{s}</Badge>
const PLAN_C = ['indigo', 'orange', 'teal', 'violet', 'green', 'pink', 'blue', 'amber']
const LIMIT_IC: Record<string, string> = { users: 'users', leads: 'target', automations: 'bot', sources: 'plug', searches_month: 'map', reports_month: 'file', emails_month: 'mail', calls_month: 'phone' }
const GW: Record<string, string> = { razorpay: 'Razorpay', stripe: 'Stripe' }
const PAY_C: Record<string, string> = { paid: 'green', captured: 'green', succeeded: 'green', pending: 'gold', created: 'gold', failed: 'red', cancelled: 'gray', canceled: 'gray', refunded: 'blue' }
const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : '—')

/** Billing state of an organisation from its dates. */
function planState(o: { trial_ends_at?: string | null; plan_expires_at?: string | null; plan_status?: string }) {
  if (o.plan_status) return o.plan_status
  if (future(o.trial_ends_at)) return 'trial'
  if (future(o.plan_expires_at)) return 'active'
  if (o.trial_ends_at || o.plan_expires_at) return 'expired'
  return ''
}
function PlanState({ o }: { o: any }) {
  const s = planState(o)
  if (s === 'trial') return <Badge c="gold">Trial to {date(o.trial_ends_at)}</Badge>
  if (s === 'expired') return <Badge c="red">Expired</Badge>
  if (s === 'active' && o.plan_expires_at) return <Badge c="green">Paid to {date(o.plan_expires_at)}</Badge>
  return null
}

function usePayments() {
  return useQuery({ queryKey: ['p-payments'], queryFn: () => api.get<any[]>('/platform/payments').then(r => (Array.isArray(r) ? r : [])), retry: false })
}
const isPaid = (s: string) => ['paid', 'captured', 'succeeded'].includes(s)

// ------------------------------------------------------------------ overview
export function Overview() {
  const brand = useBrand()
  usePageTitle('Platform overview', 'Every organisation on ' + brand.name)
  const q = useQuery({ queryKey: ['p-stats'], queryFn: () => api.get<any>('/platform/stats'), refetchInterval: 60_000 })
  const orgs = useQuery({ queryKey: ['p-orgs', '', '', ''], queryFn: () => api.get<any[]>('/platform/orgs', {}) })
  const pays = usePayments()
  if (!q.data) return <><div className="kpis">{[0, 1, 2, 3].map(i => <div key={i} className="stat"><Skeleton h={90} /></div>)}</div><div className="card"><Skeleton h={240} /></div></>
  const s = q.data.stats || {}, sign: { day: string; n: number }[] = q.data.signups || [], by: { plan: string; n: number; mrr: number }[] = q.data.by_plan || []
  const mrr = by.reduce((t, p) => t + (p.mrr || 0), 0)
  const in30 = sign.reduce((a, b) => a + b.n, 0)
  let run = Math.max(0, (s.orgs || 0) - in30)
  const cum = sign.map(x => (run += x.n))
  const paid = (pays.data || []).filter(p => isPaid(p.status))
  const month = new Date(); month.setDate(1); month.setHours(0, 0, 0, 0)
  const thisMonth = paid.filter(p => new Date(p.paid_at || p.created_at) >= month)
  const cur = paid[0]?.currency || 'INR'
  const collected = thisMonth.reduce((a, p) => a + (p.currency === cur ? p.amount : 0), 0)
  const mrrMax = Math.max(1, ...by.map(p => p.mrr || 0))
  const h = new Date().getHours()
  return <>
    <div className="pc-hero card dk">
      <div className="grow">
        <span className="eyebrow"><Icon n="spark" />{h < 12 ? 'Good morning' : h < 17 ? 'Good afternoon' : 'Good evening'}</span>
        <h2>{num(s.orgs)} organisations run on <span>{brand.name}</span></h2>
        <p>{s.new_month || 0} joined this month · {s.trials || 0} on trial · {s.active_users || 0} people signed in today</p>
        <div className="row mt"><Link className="btn btn-p" to="/admin/orgs"><Icon n="users" /><span>Organisations</span></Link><Link className="btn" to="/admin/plans"><Icon n="tag" /><span>Plans</span></Link><Link className="btn" to="/admin/payments"><Icon n="crown" /><span>Payments</span></Link></div>
      </div>
      <RocketRider w={210} />
    </div>
    <div className="kpis pc-kpis">
      <Kpi label="Organisations" value={num(s.orgs)} ic="grid" c="indigo" tone="pri" to="/admin/orgs" spark={cum} foot={<><span className="trend up">+{in30}</span> in 30 days</>} />
      <Kpi label="Active" value={num(s.active)} ic="check" c="green" foot={(s.suspended || 0) + ' suspended · ' + (s.trials || 0) + ' on trial'} />
      <Kpi label="Users" value={num(s.users)} ic="users" c="blue" foot={(s.active_users || 0) + ' signed in today'} />
      <Kpi label="Monthly revenue (list)" value={'₹' + num(mrr)} ic="trendUp" c="green" tone="ok" foot={pays.data ? sym(cur) + num(collected) + ' collected this month' : 'sum of active plan prices'} />
    </div>
    <div className="dg eq">
      <Card title="Sign-ups – last 30 days" ic="activity" c="violet" actions={<span className="pc-big">{in30}<small> new</small></span>}>
        {in30 === 0 ? <EmptyState art={<SleepyCat />} title="No sign-ups in 30 days">New organisations appear here day by day.</EmptyState>
          : <BarChart c="indigo" height={210} every={5} data={sign.map(x => ({ label: new Date(x.day).toLocaleDateString('en-GB', { day: '2-digit', month: 'short' }), value: x.n, hint: new Date(x.day).toLocaleDateString('en-GB', { weekday: 'long', day: '2-digit', month: 'short' }) }))} />}
      </Card>
      <Card title="Organisations by plan" ic="tag" c="orange" actions={<Link className="lnk sm" to="/admin/plans">Plans <Icon n="arrowR" /></Link>}>
        {!by.length ? <EmptyState art={<SleepyCat />} title="No active organisations" /> : <>
          <Donut size={150} thick={18} center={<><b>{num(by.reduce((a, b) => a + b.n, 0))}</b><small>active</small></>} data={by.map((p, i) => ({ label: p.plan, value: p.n, c: PLAN_C[i % PLAN_C.length] }))} legend={false} />
          <div className="pc-pb">{by.map((p, i) => <div key={p.plan} className={'c-' + PLAN_C[i % PLAN_C.length]}>
            <span className="pc-pb-n"><i />{p.plan}<em>{p.n} org{p.n === 1 ? '' : 's'}</em></span>
            <span className="pc-pb-t"><i style={{ width: Math.max(2, (p.mrr / mrrMax) * 100) + '%' }} /></span><b>₹{num(p.mrr)}</b></div>)}</div>
        </>}
      </Card>
    </div>
    <div className="dg">
      <Card p0 title="Newest organisations" ic="users" c="green" actions={<Link className="lnk sm" to="/admin/orgs">All <Icon n="arrowR" /></Link>}>
        {!orgs.data ? <Spinner /> : !orgs.data.length ? <EmptyState art={<SleepyCat />} title="No organisations yet">They appear here when customers sign up.</EmptyState> :
          <div className="pc-list">{orgs.data.slice(0, 6).map(o => <Link key={o.id} to={'/admin/orgs/' + o.id} className="pc-li">
            <Avatar name={o.name} size={38} /><span className="grow"><b>{o.name}</b><small>{o.owner || o.slug} · joined {ago(o.created_at)}</small></span>
            <span className="pc-li-r">{o.plan && <Badge c="gray">{o.plan}</Badge>}<PlanState o={o} />{o.status !== 'active' && statusBadge(o.status)}</span><Icon n="chevR" c="pc-chev" /></Link>)}</div>}
      </Card>
      <div className="col">
        <div className="card dk c-teal pc-jobs">
          <div className="card-h"><h3><Hi n="zap" c="teal" />Background work</h3></div>
          <div className="pc-jobs-g"><div><b>{num(s.jobs_pending)}</b><span>jobs waiting</span></div><div className={s.jobs_failed ? 'bad' : ''}><b>{num(s.jobs_failed)}</b><span>failed (24 h)</span></div></div>
        </div>
        <Card title="Latest payments" ic="crown" c="amber" actions={<Link className="lnk sm" to="/admin/payments">All <Icon n="arrowR" /></Link>}>
          {!pays.data?.length ? <p className="muted sm mb0">{pays.isError ? 'Payments are not available on this server yet.' : 'No payments yet.'}</p> :
            <div className="pc-list tight">{pays.data.slice(0, 4).map(p => <div key={p.id} className="pc-li"><Avatar name={p.org} size={30} /><span className="grow"><b>{p.org}</b><small>{p.plan} · {dateTime(p.paid_at || p.created_at)}</small></span><span className="pc-li-r"><b>{price(p.amount, p.currency)}</b><Badge c={PAY_C[p.status] || 'gray'} dot>{cap(p.status)}</Badge></span></div>)}</div>}
        </Card>
      </div>
    </div>
  </>
}

// ------------------------------------------------------------------ organisations
const SF: [string, string][] = [['', 'All'], ['active', 'Active'], ['trial', 'On trial'], ['expired', 'Expired'], ['suspended', 'Suspended']]
export function Orgs() {
  usePageTitle('Organisations', 'Customers using this CRM')
  const [q, setQ] = useState(''), [status, setStatus] = useState(''), [plan, setPlan] = useState('')
  const plans = useQuery({ queryKey: ['p-plans'], queryFn: () => api.get<any>('/platform/plans') })
  const r = useQuery({ queryKey: ['p-orgs', q, '', plan], queryFn: () => api.get<any[]>('/platform/orgs', { q, plan }) })
  const go = useNavigate()
  const all = r.data || []
  const match = (o: any, k: string) => !k || (k === 'active' || k === 'suspended' ? o.status === k : planState(o) === k)
  const rows = all.filter(o => match(o, status))
  return <>
    <div className="fbar pc-fbar">
      <label className="srch"><Icon n="search" /><input placeholder="Search name, short name or email…" value={q} onChange={e => setQ(e.target.value)} aria-label="Search organisations" /></label>
      <div className="seg" role="group" aria-label="Status">{SF.map(([k, l]) => <button key={k} type="button" className={status === k ? 'on' : ''} onClick={() => setStatus(k)}>{l}<em className="pc-n">{all.filter(o => match(o, k)).length}</em></button>)}</div>
      <select value={plan} onChange={e => setPlan(e.target.value)} aria-label="Plan"><option value="">All plans</option>{plans.data?.plans?.map((p: any) => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
      {(q || status || plan) && <button type="button" className="clr-f btn-ghost" onClick={() => { setQ(''); setStatus(''); setPlan('') }}>Clear</button>}
    </div>
    <Card p0 title={<>Organisations <Badge c="gray">{rows.length}</Badge></>} ic="users" c="green">
      {!r.data ? <Spinner /> : !rows.length ? (all.length ? <EmptyState art={<Detective />} title="No organisation matches">Try another search or clear the filters.</EmptyState> : <EmptyState art={<SleepyCat />} title="No organisations yet">Organisations appear here when customers sign up.</EmptyState>) :
        <div className="tw"><table className="tbl pc-tbl"><thead><tr><th>Organisation</th><th className="hm">Owner</th><th>Plan</th><th className="num hm">Users</th><th>Status</th><th className="hm">Joined</th><th className="hm">Last seen</th><th aria-label="Open" /></tr></thead>
          <tbody>{rows.map(o => <tr key={o.id} className="pc-clk" tabIndex={0} onClick={() => go('/admin/orgs/' + o.id)} onKeyDown={e => e.key === 'Enter' && go('/admin/orgs/' + o.id)}>
            <td><div className="who"><Avatar name={o.name} size={36} /><div className="biz"><b>{o.name}</b><small>#{o.id} · {o.slug}</small></div></div></td>
            <td className="ell hm" style={{ maxWidth: 220 }}>{o.owner || '—'}</td>
            <td><div className="pc-plan-c"><b>{o.plan || '—'}</b><PlanState o={o} /></div></td>
            <td className="num hm">{o.users}</td><td>{statusBadge(o.status)}</td><td className="nowrap hm">{date(o.created_at)}</td><td className="nowrap hm">{ago(o.last_seen)}</td>
            <td className="act"><Icon n="chevR" c="pc-chev" /></td></tr>)}</tbody></table></div>}
    </Card>
  </>
}

export function OrgDetail() {
  const { id } = useParams()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['p-org', id], queryFn: () => api.get<any>('/platform/orgs/' + id) })
  const plans = useQuery({ queryKey: ['p-plans'], queryFn: () => api.get<any>('/platform/plans') })
  const pays = usePayments()
  const [f, setF] = useState<any>(null)
  const [busy, setBusy] = useState(false)
  usePageTitle(q.data?.org.name || 'Organisation', 'Plan, modules, limits and access')
  useEffect(() => {
    if (!q.data) return
    const o = q.data.org
    setF({ name: o.name, status: o.status, plan_id: o.plan_id || '', notes: o.notes || '', trial: dateInput(o.trial_ends_at), expires: dateInput(o.plan_expires_at),
      customMods: !!o.modules, modules: o.modules || o.plan_modules, customLimits: !!o.limits, limits: o.limits || {} })
  }, [q.data])
  if (q.isError) return <EmptyState art={<Detective />} title="Organisation not found" action={<Link className="btn" to="/admin/orgs"><Icon n="arrowL" /><span>Organisations</span></Link>} />
  if (!q.data || !f) return <><div className="card dk"><Skeleton h={110} /></div><div className="g3r"><div className="card"><Skeleton h={300} /></div><div className="card"><Skeleton h={300} /></div></div></>
  const o = q.data.org, keys: LimitKey[] = q.data.limit_keys || [], usage = q.data.usage || {}
  const plan = plans.data?.plans?.find((p: any) => p.id === Number(f.plan_id))
  const planLimits = plan?.limits || o.plan_limits || {}
  const owner = (q.data.members || []).find((m: any) => m.role === 'super')
  const orgPays = (pays.data || []).filter(p => p.org_id === o.id)
  const save = async (e?: FormEvent) => {
    e?.preventDefault(); setBusy(true)
    await act(() => api.put('/platform/orgs/' + id, { name: f.name, status: f.status, plan_id: Number(f.plan_id) || 0, notes: f.notes, trial_ends_at: toISO(f.trial), plan_expires_at: toISO(f.expires),
      modules: f.customMods ? f.modules : null, limits: f.customLimits ? f.limits : null }), 'Saved.')
    setBusy(false)
    qc.invalidateQueries({ queryKey: ['p-org', id] }); qc.invalidateQueries({ queryKey: ['p-orgs'] })
  }
  const enter = async () => { const r = await act(() => api.post('/platform/orgs/' + id + '/enter')); if (r) location.href = '/dashboard' }
  const mod = (m: string, on: boolean) => setF({ ...f, modules: on ? [...new Set([...(f.modules || []), m])] : (f.modules || []).filter((x: string) => x !== m) })
  const effMods: string[] = f.customMods ? f.modules || [] : plan?.modules || o.plan_modules || ['crm']
  return <form onSubmit={save}>
    <Link className="back pc-back" to="/admin/orgs"><Icon n="arrowL" />Organisations</Link>
    <div className="card dk pc-oh">
      <Avatar name={o.name} size={60} />
      <div className="grow">
        <h2>{o.name} {statusBadge(o.status)}</h2>
        <div className="meta"><span><Icon n="link" />{o.slug}</span><span><Icon n="tag" />{o.plan || 'No plan'}</span><span><Icon n="cal" />Joined {date(o.created_at)}</span>{owner && <span><Icon n="user" />{owner.email}</span>}</div>
        <div className="row pc-oh-b"><PlanState o={o} /><Badge c="blue">{(q.data.members || []).length} member{(q.data.members || []).length === 1 ? '' : 's'}</Badge>{effMods.includes('prospecting') && <Badge c="orange">Prospecting</Badge>}</div>
      </div>
      <div className="pha">{o.status === 'active' && <Btn ic="logout" onClick={enter}>Open workspace</Btn>}<Btn v="p" type="submit" ic="check" busy={busy}>Save changes</Btn></div>
    </div>
    {o.status === 'active' && <Alert>Opening the workspace signs you in as its owner for support. You are not added to its team and its users never see you.</Alert>}
    <div className="g3r">
      <div className="col">
        <div className="card"><Sec ic="grid" c="indigo">Organisation &amp; plan</Sec><div className="fg">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Status"><select value={f.status} onChange={e => setF({ ...f, status: e.target.value })}><option value="active">Active</option><option value="suspended">Suspended (blocks sign-in)</option><option value="deleted">Deleted (hidden)</option></select></Field>
          <Field label="Plan"><select value={f.plan_id} onChange={e => setF({ ...f, plan_id: e.target.value })}><option value="">No plan</option>{plans.data?.plans?.map((p: any) => <option key={p.id} value={p.id}>{p.name} – {price(p.price_month, p.currency)}/month</option>)}</select></Field>
          <Field label="Trial ends" hint="Empty = no trial"><input type="date" value={f.trial} onChange={e => setF({ ...f, trial: e.target.value })} /></Field>
          <Field label="Paid until" hint="Set automatically by online payments"><input type="date" value={f.expires} onChange={e => setF({ ...f, expires: e.target.value })} /></Field>
          <Field label="Internal notes" hint="Only platform admins see these"><textarea rows={3} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
        </div></div>
        <div className="card"><Sec ic="sliders" c="orange">Limits &amp; usage</Sec>
          <label className="pc-sw"><Switch checked={f.customLimits} onChange={v => setF({ ...f, customLimits: v })} /><span><b>Custom limits</b><small>Empty = plan value, 0 = unlimited</small></span></label>
          <div className="pc-lims">{keys.map(([k, l]) => {
            const lim = f.customLimits && f.limits[k] !== undefined ? f.limits[k] : planLimits[k] || 0
            const pc = lim ? Math.round((usage[k] || 0) / lim * 100) : 0
            return <div key={k} className="pc-lim">
              <Progress value={usage[k] || 0} max={lim || 1} c={!lim ? 'gray' : pc >= 90 ? 'rose' : pc >= 70 ? 'amber' : 'green'}
                label={<><Icon n={LIMIT_IC[k] || 'zap'} c="xs" /> {cap(l)}</>} sub={<>{num(usage[k])} / {lim ? num(lim) : '∞'}</>} />
              <input type="number" min={0} disabled={!f.customLimits} value={f.limits[k] ?? ''} placeholder={planLimits[k] ? String(planLimits[k]) : '∞'} aria-label={'Custom limit for ' + l}
                onChange={e => { const l2 = { ...f.limits }; if (e.target.value === '') delete l2[k]; else l2[k] = Number(e.target.value); setF({ ...f, limits: l2 }) }} />
            </div>
          })}</div>
        </div>
      </div>
      <div className="col">
        <div className="card"><Sec ic="layers" c="pink">Modules</Sec>
          <label className="pc-sw"><Switch checked={f.customMods} onChange={v => setF({ ...f, customMods: v })} /><span><b>Custom modules</b><small>Otherwise the plan decides</small></span></label>
          <div className="pc-mods">{[['crm', 'CRM', 'Leads, pipeline, capture, bookings, automations', 'users', 'green'], ['prospecting', 'Prospecting', 'Maps scraper, audits, AI reports, outreach', 'map', 'orange']].map(([k, l, d, ic, c]) =>
            <label key={k} className={'pc-mod c-' + c + (effMods.includes(k) ? ' on' : '')}><Hi n={ic} c={c} /><span className="grow"><b>{l}</b><small>{k === 'crm' ? 'Always on' : d}</small></span>
              <input type="checkbox" disabled={!f.customMods || k === 'crm'} checked={k === 'crm' || effMods.includes(k)} onChange={e => mod(k, e.target.checked)} /></label>)}</div>
        </div>
        <Card p0 title="Team" ic="users" c="blue">
          <div className="pc-list">{(q.data.members || []).map((m: any) => <div key={m.id} className="pc-li"><Avatar name={m.name} size={34} /><span className="grow"><b>{m.name}</b><small>{m.email}</small></span>
            <span className="pc-li-r"><Badge c={m.role === 'super' ? 'purple' : m.role === 'admin' ? 'blue' : 'green'}>{m.role === 'super' ? 'Owner' : cap(m.role)}</Badge>{!m.active && <Badge>Off</Badge>}<small className="muted nowrap">{ago(m.last_login)}</small></span></div>)}</div>
        </Card>
        {orgPays.length > 0 && <Card p0 title="Payments" ic="crown" c="amber"><div className="pc-list">{orgPays.slice(0, 5).map(p => <div key={p.id} className="pc-li"><span className="grow"><b>{p.plan} · {p.cycle === 'year' ? 'yearly' : 'monthly'}</b><small>{dateTime(p.paid_at || p.created_at)} · {GW[p.gateway] || p.gateway}</small></span><span className="pc-li-r"><b>{price(p.amount, p.currency)}</b><Badge c={PAY_C[p.status] || 'gray'} dot>{cap(p.status)}</Badge></span></div>)}</div></Card>}
      </div>
    </div>
  </form>
}

// ------------------------------------------------------------------ plans
interface Plan { id?: number; slug: string; name: string; description: string; price_month: number; price_year: number; currency: string; trial_days: number; features: string[]; limits: Record<string, number>; modules: string[]; public: boolean; is_default: boolean; highlight: boolean; sort: number; orgs?: number }
const blank: Plan = { slug: '', name: '', description: '', price_month: 0, price_year: 0, currency: 'INR', trial_days: 14, features: [], limits: {}, modules: ['crm'], public: true, is_default: false, highlight: false, sort: 10 }

function PlanCard({ p, keys, actions, preview }: { p: Plan; keys: LimitKey[]; actions?: ReactNode; preview?: boolean }) {
  const save = p.price_month > 0 && p.price_year > 0 ? Math.round((1 - p.price_year / (p.price_month * 12)) * 100) : 0
  return <div className={'prc-c pc-plan' + (p.highlight ? ' dk' : '') + (preview ? ' pv' : '')}>
    {p.highlight && <span className="rb">Most popular</span>}
    <div className="prc-n"><b>{p.name || 'Plan name'}</b><span className="row" style={{ gap: 4 }}>{p.is_default && <Badge c="blue">Default</Badge>}{!p.public && <Badge>Hidden</Badge>}</span></div>
    <div className="prc-p">{price(p.price_month, p.currency)}{p.price_month > 0 && <small>/ month</small>}</div>
    <div className="prc-s">{p.price_year > 0 ? price(p.price_year, p.currency) + ' / year' + (save > 0 ? ' · save ' + save + '%' : '') : ''}{p.trial_days > 0 ? (p.price_year > 0 ? ' · ' : '') + p.trial_days + '-day trial' : ''}</div>
    {p.description && <p className="prc-d">{p.description}</p>}
    {p.features.filter(Boolean).length > 0 && <ul className="prc-l">{p.features.filter(Boolean).map(x => <li key={x}><Icon n="check" />{x}</li>)}</ul>}
    <div className="pc-pl">{keys.map(([k, l]) => <span key={k}><Icon n={LIMIT_IC[k] || 'zap'} /><small>{l}</small>{p.limits[k] ? <b>{num(p.limits[k])}</b> : <b className="pc-inf" title="Unlimited">∞</b>}</span>)}</div>
    <div className="pc-pm">{p.modules.map(m => <Badge key={m} c={m === 'crm' ? 'green' : 'orange'}>{m === 'crm' ? 'CRM' : cap(m)}</Badge>)}</div>
    {actions}
  </div>
}

export function Plans() {
  usePageTitle('Plans & limits', 'Pricing shown on sign up and billing, and the limits each plan enforces')
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['p-plans'], queryFn: () => api.get<any>('/platform/plans') })
  const [ed, setEd] = useState<Plan | null>(null)
  if (!q.data) return <div className="prc">{[0, 1, 2].map(i => <div key={i} className="card"><Skeleton h={320} /></div>)}</div>
  const keys: LimitKey[] = q.data.limit_keys || []
  const save = async (e: FormEvent) => {
    e.preventDefault(); if (!ed) return
    const body = { ...ed, features: ed.features.filter(Boolean) }
    const r = await act(() => ed.id ? api.put('/platform/plans/' + ed.id, body) : api.post('/platform/plans', body), 'Plan saved.')
    if (r) { setEd(null); qc.invalidateQueries({ queryKey: ['p-plans'] }) }
  }
  const del = async (p: Plan) => { if (confirmDo('Delete the plan ' + p.name + '?') && await act(() => api.del('/platform/plans/' + p.id), 'Deleted.')) qc.invalidateQueries({ queryKey: ['p-plans'] }) }
  const plans: Plan[] = q.data.plans || []
  return <>
    <div className="pc-ph"><p className="muted">Limits are enforced immediately. Changing a plan affects every organisation on it; per-organisation overrides live on the organisation page.</p><Btn v="p" ic="plus" onClick={() => setEd({ ...blank })}>New plan</Btn></div>
    {!plans.length ? <div className="card"><EmptyState art={<PiggyBank />} title="No plans yet" action={<Btn v="p" ic="plus" onClick={() => setEd({ ...blank })}>Create the first plan</Btn>}>Plans set the price, trial, modules and limits for your customers.</EmptyState></div> :
      <div className="prc pc-plans">{plans.map(p => <PlanCard key={p.id} p={p} keys={keys} actions={<div className="pc-pf"><small className="grow">{p.orgs ?? 0} organisation{p.orgs === 1 ? '' : 's'}</small><Btn sm ic="edit" onClick={() => setEd({ ...p, features: p.features || [] })}>Edit</Btn><Btn sm v="ghost" ic="trash" title="Delete" onClick={() => del(p)} /></div>} />)}</div>}
    <Modal open={!!ed} onClose={() => setEd(null)} title={ed?.id ? 'Edit ' + ed.name : 'New plan'} ic="tag" c="orange" size="xl">
      {ed && <form onSubmit={save} className="pc-ed">
        <div className="pc-ed-f">
          <div className="sec"><Hi n="edit" c="indigo" /><h3>Basics</h3></div>
          <div className="fg">
            <Field label="Name"><input required value={ed.name} onChange={e => setEd({ ...ed, name: e.target.value })} /></Field>
            <Field label="Short name (URL-safe)" hint="Leave empty to create from the name"><input value={ed.slug} onChange={e => setEd({ ...ed, slug: e.target.value })} /></Field>
            <div className="full"><Field label="Description"><input value={ed.description} onChange={e => setEd({ ...ed, description: e.target.value })} /></Field></div>
          </div>
          <div className="sec"><Hi n="crown" c="amber" /><h3>Pricing</h3></div>
          <div className="fg">
            <Field label="Currency"><select value={ed.currency} onChange={e => setEd({ ...ed, currency: e.target.value })}>{['INR', 'USD', 'EUR', 'GBP', 'AED'].map(c => <option key={c}>{c}</option>)}</select></Field>
            <Field label="Free trial (days)"><input type="number" min={0} value={ed.trial_days} onChange={e => setEd({ ...ed, trial_days: Number(e.target.value) })} /></Field>
            <Field label="Price per month"><input type="number" min={0} value={ed.price_month} onChange={e => setEd({ ...ed, price_month: Number(e.target.value) })} /></Field>
            <Field label="Price per year"><input type="number" min={0} value={ed.price_year} onChange={e => setEd({ ...ed, price_year: Number(e.target.value) })} /></Field>
          </div>
          <div className="sec"><Hi n="list" c="green" /><h3>Features shown on the card</h3></div>
          <ChipsInput value={ed.features.join('\n')} onChange={v => setEd({ ...ed, features: v ? v.split('\n') : [] })} placeholder="e.g. WhatsApp & email follow-ups" label="Features" />
          <div className="sec mt"><Hi n="layers" c="pink" /><h3>Modules</h3></div>
          <div className="pc-mods">
            <label className="pc-mod c-green on"><Hi n="users" c="green" /><span className="grow"><b>CRM</b><small>Always included</small></span><input type="checkbox" checked disabled /></label>
            <label className={'pc-mod c-orange' + (ed.modules.includes('prospecting') ? ' on' : '')}><Hi n="map" c="orange" /><span className="grow"><b>Prospecting</b><small>Maps, audits, AI reports</small></span><input type="checkbox" checked={ed.modules.includes('prospecting')} onChange={e => setEd({ ...ed, modules: e.target.checked ? ['crm', 'prospecting'] : ['crm'] })} /></label>
          </div>
          <div className="sec mt"><Hi n="sliders" c="orange" /><h3>Limits <small>– empty or 0 = unlimited</small></h3></div>
          <div className="fg">{keys.map(([k, l]) => <Field key={k} label={cap(l)}><input type="number" min={0} value={ed.limits[k] || ''} placeholder="Unlimited" onChange={e => setEd({ ...ed, limits: { ...ed.limits, [k]: Number(e.target.value) || 0 } })} /></Field>)}</div>
          <div className="sec"><Hi n="eye" c="teal" /><h3>Visibility</h3></div>
          <div className="pc-sws">
            <label className="pc-sw"><Switch checked={ed.public} onChange={v => setEd({ ...ed, public: v })} /><span><b>Show on sign up &amp; billing</b><small>Hidden plans can still be assigned</small></span></label>
            <label className="pc-sw"><Switch checked={ed.is_default} onChange={v => setEd({ ...ed, is_default: v })} /><span><b>Default plan</b><small>New sign-ups and expired plans use it</small></span></label>
            <label className="pc-sw"><Switch checked={ed.highlight} onChange={v => setEd({ ...ed, highlight: v })} /><span><b>Highlight as popular</b><small>Dark card with a badge</small></span></label>
            <Field label="Order on the pricing page"><input type="number" value={ed.sort} onChange={e => setEd({ ...ed, sort: Number(e.target.value) })} /></Field>
          </div>
        </div>
        <aside className="pc-ed-p"><div className="pc-ed-s"><span className="pc-cap"><Icon n="eye" />Live preview</span><PlanCard p={ed} keys={keys} preview />
          <div className="md-f"><Btn onClick={() => setEd(null)}>Cancel</Btn><Btn v="p" type="submit" ic="check">Save plan</Btn></div></div></aside>
      </form>}
    </Modal>
  </>
}

// ------------------------------------------------------------------ payments
const PK = ['razorpay_enabled', 'razorpay_key_id', 'razorpay_key_secret', 'razorpay_webhook_secret', 'stripe_enabled', 'stripe_secret_key', 'stripe_webhook_secret']
function CopyField({ label, value, hint }: { label: string; value: string; hint?: ReactNode }) {
  const copy = () => { navigator.clipboard?.writeText(value).then(() => toast('Copied.'), () => toast('Copy failed – select the text instead.')) }
  return <Field label={label} hint={hint}><span className="copy"><input readOnly value={value} onFocus={e => e.target.select()} /><Btn sm ic="copy" onClick={copy}>Copy</Btn></span></Field>
}

export function Payments() {
  usePageTitle('Payments', 'Payment gateways for plan purchases, and every payment')
  const qc = useQueryClient()
  const s = useQuery({ queryKey: ['p-pay-set'], queryFn: () => api.get<any>('/platform/payments/settings'), retry: false })
  const pays = usePayments()
  const [f, setF] = useState<Record<string, string> | null>(null)
  const [busy, setBusy] = useState(false)
  const [st, setSt] = useState('')
  useEffect(() => { if (s.data) setF(Object.fromEntries(PK.map(k => [k, String(s.data[k] ?? '')]))) }, [s.data])
  const all = pays.data || []
  const rows = st ? all.filter(p => (st === 'paid' ? isPaid(p.status) : p.status === st)) : all
  const paid = all.filter(p => isPaid(p.status))
  const cur = paid[0]?.currency || 'INR'
  const total = paid.reduce((a, p) => a + (p.currency === cur ? p.amount : 0), 0)
  const m0 = new Date(); m0.setDate(1); m0.setHours(0, 0, 0, 0)
  const month = paid.filter(p => new Date(p.paid_at || p.created_at) >= m0).reduce((a, p) => a + (p.currency === cur ? p.amount : 0), 0)
  const save = async (e: FormEvent) => {
    e.preventDefault(); if (!f) return
    setBusy(true)
    const r = await act(() => api.put('/platform/payments/settings', f), 'Payment settings saved.')
    setBusy(false)
    if (r) { qc.setQueryData(['p-pay-set'], r); qc.invalidateQueries({ queryKey: ['p-pay-set'] }) }
  }
  const on = (k: string) => f?.[k] === '1' || f?.[k] === 'true'
  const set = (k: string, v: string) => f && setF({ ...f, [k]: v })
  const gws: [string, string, string, string, [string, string, boolean][]][] = [
    ['razorpay', 'Razorpay', 'UPI, cards, net banking & wallets – Payment Links (India)', 'blue', [['razorpay_key_id', 'Key ID', false], ['razorpay_key_secret', 'Key secret', true], ['razorpay_webhook_secret', 'Webhook secret', true]]],
    ['stripe', 'Stripe', 'International cards – Stripe Checkout', 'violet', [['stripe_secret_key', 'Secret key', true], ['stripe_webhook_secret', 'Webhook signing secret', true]]],
  ]
  return <>
    <div className="stats">
      <Kpi label="Collected (all time)" value={sym(cur) + num(total)} ic="crown" c="amber" tone="pri" foot={paid.length + ' successful payment' + (paid.length === 1 ? '' : 's')} />
      <Kpi label="This month" value={sym(cur) + num(month)} ic="trendUp" c="green" tone="ok" foot={new Date().toLocaleDateString('en-GB', { month: 'long', year: 'numeric' })} />
      <Kpi label="Pending" value={num(all.filter(p => ['pending', 'created'].includes(p.status)).length)} ic="clock" c="amber" foot="waiting for the gateway" />
      <Kpi label="Failed" value={num(all.filter(p => p.status === 'failed').length)} ic="alert" c="rose" foot="customer can retry" />
    </div>
    {s.isError ? <Alert t="warn">Payment settings are not available on this server version yet.</Alert> : !f ? <div className="g2"><div className="card"><Skeleton h={260} /></div><div className="card"><Skeleton h={260} /></div></div> :
      <form onSubmit={save}>
        <div className="g2 pc-gws">{gws.map(([k, name, desc, c, fields]) => <div key={k} className={'card p0 pc-gw c-' + c + (on(k + '_enabled') ? ' on' : '')}>
          <div className="pc-gw-h dk"><span className="pc-gw-l" style={{ background: k === 'stripe' ? '#635BFF' : '#2B6CE0' }}>{name[0]}</span><span className="grow"><b>{name}</b><small>{desc}</small></span>
            <span className="pc-gw-s">{on(k + '_enabled') ? <Badge c="green" dot>Enabled</Badge> : <Badge>Off</Badge>}<Switch checked={on(k + '_enabled')} onChange={v => set(k + '_enabled', v ? '1' : '0')} title={'Enable ' + name} /></span></div>
          <div className="pc-gw-b">
            {fields.map(([fk, l, secret]) => <Field key={fk} label={l}>{secret ? <Secret value={f[fk]} onChange={e => set(fk, e.target.value)} placeholder="Not set" /> : <input value={f[fk]} onChange={e => set(fk, e.target.value)} placeholder="Not set" />}</Field>)}
            {s.data?.webhooks?.[k] && <CopyField label="Webhook URL" value={s.data.webhooks[k]} hint={<>Add it in the {name} dashboard{s.data?.[k + '_events'] ? <> for the event <code>{s.data[k + '_events']}</code></> : ''}.</>} />}
          </div>
        </div>)}</div>
        <div className="pc-save"><Btn v="p" type="submit" ic="check" busy={busy}>Save payment settings</Btn><span className="muted sm">Keys are stored encrypted. Leave a secret unchanged to keep the saved one.</span></div>
      </form>}
    <Card p0 title={<>All payments <Badge c="gray">{rows.length}</Badge></>} ic="file" c="violet" actions={<div className="seg" role="group" aria-label="Filter payments">{[['', 'All'], ['paid', 'Paid'], ['pending', 'Pending'], ['failed', 'Failed']].map(([k, l]) => <button key={k} type="button" className={st === k ? 'on' : ''} onClick={() => setSt(k)}>{l}</button>)}</div>}>
      {pays.isLoading ? <Spinner /> : !rows.length ? <EmptyState art={<PiggyBank />} title={pays.isError ? 'Payments are not available yet' : 'No payments yet'}>Plan purchases by organisations appear here as soon as a gateway confirms them.</EmptyState> :
        <div className="tw"><table className="tbl"><thead><tr><th>Date</th><th>Organisation</th><th>Plan</th><th>Period</th><th className="num">Amount</th><th>Gateway</th><th>Status</th><th>Paid on</th></tr></thead>
          <tbody>{rows.map(p => <tr key={p.id}>
            <td className="nowrap">{date(p.created_at)}</td>
            <td><Link to={'/admin/orgs/' + p.org_id} className="who"><Avatar name={p.org || '?'} size={30} /><span className="biz"><b>{p.org}</b>{p.by_email && <small>{p.by_email}</small>}</span></Link></td>
            <td>{p.plan || '—'}</td><td>{p.cycle === 'year' ? 'Yearly' : 'Monthly'}</td>
            <td className="num nowrap"><b>{price(p.amount, p.currency)}</b></td>
            <td><span className="pc-gwb">{GW[p.gateway] || p.gateway}</span></td>
            <td><Badge c={PAY_C[p.status] || 'gray'} dot>{cap(p.status)}</Badge></td>
            <td className="nowrap">{p.paid_at ? dateTime(p.paid_at) : '—'}</td></tr>)}</tbody></table></div>}
    </Card>
  </>
}

// ------------------------------------------------------------------ branding
const FONTS = ['Plus Jakarta Sans', 'Inter', 'Poppins', 'Manrope', 'DM Sans', 'Outfit', 'Sora', 'Urbanist', 'Figtree', 'Lexend', 'Montserrat', 'Nunito Sans', 'Roboto', 'Open Sans', 'Lato', 'Work Sans', 'Rubik', 'Space Grotesk', 'Raleway', 'Mulish']
const COLORS: [string, string][] = [['brand_primary', 'Primary'], ['brand_accent', 'Accent (call-to-action)'], ['brand_success', 'Success'], ['brand_warning', 'Warning'], ['brand_danger', 'Danger'], ['brand_info', 'Info']]
const DEF = { brand_primary: '#C21F3A', brand_accent: '#FFD60A', brand_success: '#6BA539', brand_warning: '#F59E0B', brand_danger: '#E5484D', brand_info: '#3B82F6', brand_font_heading: 'Plus Jakarta Sans', brand_font_body: 'Inter', brand_radius: '16' }

function Upload({ k, label, value, dark, onDone }: { k: string; label: string; value: string; dark?: boolean; onDone: (url: string) => void }) {
  const up = async (file?: File) => { if (!file) return; const r = await act(() => api.upload<{ url: string }>('/platform/files', { file, setting: k }), 'Uploaded.'); if (r) onDone(r.url) }
  return <Field label={label}><div className="pc-up">
    <span className={'pc-up-i' + (dark ? ' dk' : '')}>{value ? <img src={value} alt="" /> : <Icon n="image" />}</span>
    <span className="grow"><input type="file" accept=".png,.jpg,.jpeg,.webp,.svg" onChange={e => up(e.target.files?.[0])} /></span>
    {value && <Btn sm v="ghost" ic="trash" title="Remove" onClick={() => onDone('')} />}</div></Field>
}

export function Branding() {
  usePageTitle('Branding', 'White label: your CRM name, logo, typography and colours')
  const { mode } = useTheme()
  const q = useQuery({ queryKey: ['p-brand'], queryFn: () => api.get<any>('/platform/brand') })
  const [f, setF] = useState<Record<string, string> | null>(null)
  const [pm, setPm] = useState<'light' | 'dark'>(mode)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!q.data) return
    const b = q.data
    setF({ brand_name: b.name, brand_tagline: b.tagline, brand_logo: b.logo, brand_logo_dark: b.logo_dark, brand_favicon: b.favicon, brand_font_heading: b.font_heading, brand_font_body: b.font_body,
      brand_radius: String(b.radius), brand_theme: b.theme_default, brand_support_email: b.support_email || '', brand_website: b.website || '',
      brand_primary: b.colors.primary, brand_accent: b.colors.accent, brand_success: b.colors.success, brand_warning: b.colors.warning, brand_danger: b.colors.danger, brand_info: b.colors.info })
  }, [q.data])
  // load the chosen Google fonts for the preview
  const fh = f?.brand_font_heading, fb = f?.brand_font_body
  useEffect(() => {
    if (!fh || !fb) return
    const fam = (x: string) => 'family=' + encodeURIComponent(x).replace(/%20/g, '+') + ':wght@400;600;700;800'
    let l = document.getElementById('pv-fonts') as HTMLLinkElement | null
    if (!l) { l = document.createElement('link'); l.id = 'pv-fonts'; l.rel = 'stylesheet'; document.head.appendChild(l) }
    l.href = 'https://fonts.googleapis.com/css2?' + [fam(fh), fam(fb)].join('&') + '&display=swap'
  }, [fh, fb])
  if (!f) return <div className="g3r"><div className="card"><Skeleton h={420} /></div><div className="card"><Skeleton h={420} /></div></div>
  const set = (k: string, v: string) => setF({ ...f, [k]: v })
  const save = async (e: FormEvent) => { e.preventDefault(); setBusy(true); if (await act(() => api.put('/platform/brand', f), 'Branding saved – reloading to apply.')) setTimeout(() => location.reload(), 600); else setBusy(false) }
  const hex = (v: string) => /^#[0-9a-f]{6}$/i.test(v) ? v : '#000000'
  const pv = { '--pri': f.brand_primary, '--pri-ink': inkOn(f.brand_primary), '--acc': f.brand_accent, '--acc-ink': inkOn(f.brand_accent), '--ok': f.brand_success, '--warn': f.brand_warning, '--err': f.brand_danger, '--info': f.brand_info,
    '--r': f.brand_radius + 'px', '--fh': `'${f.brand_font_heading}', sans-serif`, '--ff': `'${f.brand_font_body}', sans-serif` } as CSSProperties
  const logo = pm === 'dark' ? f.brand_logo_dark || f.brand_logo : f.brand_logo || f.brand_logo_dark
  const sbLogo = f.brand_logo_dark || f.brand_logo
  const mono = (f.brand_name.trim()[0] || 'S').toUpperCase()
  return <form onSubmit={save} className="g3r pc-br">
    <div className="col">
      <div className="card"><Sec ic="grid" c="indigo">Identity</Sec><div className="fg">
        <Field label="CRM name" hint="Shown everywhere: sign-in, menus, emails, browser tab"><input required value={f.brand_name} onChange={e => set('brand_name', e.target.value)} /></Field>
        <Field label="Tagline"><input value={f.brand_tagline} onChange={e => set('brand_tagline', e.target.value)} /></Field>
        <Field label="Support email"><input type="email" value={f.brand_support_email} onChange={e => set('brand_support_email', e.target.value)} /></Field>
        <Field label="Website"><input type="url" value={f.brand_website} onChange={e => set('brand_website', e.target.value)} /></Field>
        <Upload k="brand_logo" label="Logo for light backgrounds" value={f.brand_logo} onDone={u => set('brand_logo', u)} />
        <Upload k="brand_logo_dark" label="Logo for dark backgrounds" value={f.brand_logo_dark} dark onDone={u => set('brand_logo_dark', u)} />
        <Upload k="brand_favicon" label="Favicon (square PNG)" value={f.brand_favicon} onDone={u => set('brand_favicon', u)} />
      </div></div>
      <div className="card"><Sec ic="image" c="pink">Colours</Sec>
        <div className="pc-sws2">{COLORS.map(([k, l]) => <label key={k} className="pc-clr"><span className="pc-clr-s" style={{ background: f[k] }}><input type="color" value={hex(f[k])} onChange={e => set(k, e.target.value)} aria-label={l} /></span>
          <span className="grow"><b>{l}</b><input value={f[k]} onChange={e => set(k, e.target.value)} aria-label={l + ' hex'} /></span></label>)}</div>
        <div className="card-f"><Btn sm v="ghost" ic="refresh" onClick={() => setF({ ...f, ...DEF })}>Reset to the default palette</Btn></div></div>
      <div className="card"><Sec ic="edit" c="violet">Typography &amp; shape</Sec><div className="fg">
        <Field label="Heading font"><select value={f.brand_font_heading} onChange={e => set('brand_font_heading', e.target.value)}>{FONTS.map(x => <option key={x}>{x}</option>)}</select></Field>
        <Field label="Body font"><select value={f.brand_font_body} onChange={e => set('brand_font_body', e.target.value)}>{FONTS.map(x => <option key={x}>{x}</option>)}</select></Field>
        <Field label={'Corner radius: ' + f.brand_radius + 'px'}><input type="range" className="pc-rng" min={4} max={28} value={f.brand_radius} onChange={e => set('brand_radius', e.target.value)} /></Field>
        <Field label="Default theme"><select value={f.brand_theme} onChange={e => set('brand_theme', e.target.value)}><option value="system">Follow the device</option><option value="light">Light</option><option value="dark">Dark</option></select></Field>
      </div></div>
    </div>
    <div className="pc-pvw">
      <div className="pc-pvh"><span className="pc-cap"><Icon n="eye" />Live preview</span>
        <div className="seg" role="group" aria-label="Preview theme"><button type="button" className={pm === 'light' ? 'on' : ''} onClick={() => setPm('light')}><Icon n="sun" />Light</button><button type="button" className={pm === 'dark' ? 'on' : ''} onClick={() => setPm('dark')}><Icon n="moon" />Dark</button></div></div>
      <div className="pc-pv" data-theme={pm} style={pv}>
        <div className="pc-pv-sb">
          <div className="pc-pv-b">{sbLogo ? <img src={sbLogo} alt="" className="logo-i" style={{ width: 30, height: 30 }} /> : <span className="logo-m" style={{ width: 30, height: 30, fontSize: 15 }}>{mono}<i /></span>}<span><b>{f.brand_name}</b><small>{f.brand_tagline}</small></span></div>
          {[['grid', 'Dashboard', true], ['users', 'Leads', false], ['mail', 'Emails', false], ['cal', 'Appointments', false]].map(([ic, l, a]) => <span key={l as string} className={'pc-pv-n' + (a ? ' on' : '')}><Icon n={ic as string} />{l}</span>)}
        </div>
        <div className="pc-pv-m">
          <div className="pc-pv-t">{logo && <img src={logo} alt="" style={{ height: 22 }} />}<b>Good morning, Asha</b></div>
          <div className="pc-pv-k">
            <div className="stat tone-pri"><span className="st-i"><Icon n="users" /></span><span className="st-l">Total leads</span><b className="st-v">1,284</b><small className="st-f"><span className="trend up">+18%</span> this week</small></div>
            <div className="stat st-green"><span className="st-i"><Icon n="target" /></span><span className="st-l">Won</span><b className="st-v">₹4.2L</b><small className="st-f">12 deals</small></div>
          </div>
          <div className="card pc-pv-c"><div className="row between"><b>Next steps</b><span className="bdg bdg-dot c-green">On track</span></div>
            <div className="pc-pv-bars">{[38, 62, 45, 80, 56, 92, 70].map((h, i) => <i key={i} style={{ height: h + '%' }} className={i === 5 ? 'on' : ''} />)}</div>
            <div className="row"><span className="btn btn-p btn-sm">Confirm order</span><span className="btn btn-sm">Later</span><span className="bdg bdg-dot c-amber">Pending</span><span className="bdg c-rose">Cancelled</span></div></div>
        </div>
      </div>
      <div className="pc-save"><Btn v="p" type="submit" ic="check" busy={busy}>Save branding</Btn><span className="muted sm">Applies to every page after saving.</span></div>
    </div>
  </form>
}

// ------------------------------------------------------------------ email (platform mailer)
export function Mail() {
  usePageTitle('Email', 'Default mailer for every organisation without its own email set-up')
  const q = useQuery({ queryKey: ['p-mail'], queryFn: () => api.get<Record<string, string>>('/platform/mail') })
  const [f, setF] = useState<Record<string, string> | null>(null)
  const [to, setTo] = useState('')
  const [busy, setBusy] = useState('')
  useEffect(() => { if (q.data) setF(q.data) }, [q.data])
  if (!f) return <div className="g3r"><div className="card"><Skeleton h={360} /></div><div className="card"><Skeleton h={200} /></div></div>
  const set = (k: string, v: string) => setF({ ...f, [k]: v })
  const save = async (e: FormEvent) => { e.preventDefault(); setBusy('save'); await act(() => api.put('/platform/mail', f), 'Email settings saved.'); setBusy('') }
  const test = async () => { setBusy('test'); const r = await act(() => api.post('/platform/mail/test', { to })); setBusy(''); if (r) toast(r.message) }
  const hostinger = () => setF({ ...f, mail_mode: 'smtp', mail_smtp_host: 'smtp.hostinger.com', mail_smtp_port: '465', mail_smtp_secure: 'ssl' })
  const modes: [string, string, string, string][] = [['sendmail', 'Server mail', 'sendmail / Postfix – like PHP mail()', 'db'], ['smtp', 'SMTP mailbox', 'Recommended for delivery', 'mail']]
  return <form onSubmit={save} className="g3r">
    <div className="card"><Sec ic="mail" c="blue">Default mailer</Sec>
      <Alert>Organisations send with this mailer until they connect their own SMTP mailbox or email service in Settings → Email. Emails show the organisation's name and replies go to the organisation's email.</Alert>
      <div className="pc-opts" role="radiogroup" aria-label="How to send">{modes.map(([k, l, d, ic]) => <button key={k} type="button" role="radio" aria-checked={f.mail_mode === k} className={'pc-opt' + (f.mail_mode === k ? ' on' : '')} onClick={() => set('mail_mode', k)}><Hi n={ic} c={k === 'smtp' ? 'green' : 'gray'} /><span className="grow"><b>{l}</b><small>{d}</small></span><i /></button>)}</div>
      <div className="fg">
        <Field label="From email" hint="An address on your own domain, e.g. no-reply@yourdomain.com"><input type="email" value={f.mail_from_email} onChange={e => set('mail_from_email', e.target.value)} /></Field>
        <Field label="From name (fallback)"><input value={f.mail_from_name} onChange={e => set('mail_from_name', e.target.value)} /></Field>
        {f.mail_mode === 'sendmail' ? <Field label="sendmail path"><input value={f.mail_sendmail_path} onChange={e => set('mail_sendmail_path', e.target.value)} /></Field> : <>
          <Field label="SMTP server"><input value={f.mail_smtp_host} onChange={e => set('mail_smtp_host', e.target.value)} placeholder="smtp.hostinger.com" /></Field>
          <Field label="Port"><input value={f.mail_smtp_port} onChange={e => set('mail_smtp_port', e.target.value)} /></Field>
          <Field label="Encryption"><select value={f.mail_smtp_secure} onChange={e => set('mail_smtp_secure', e.target.value)}><option value="ssl">SSL (465)</option><option value="tls">STARTTLS (587)</option><option value="none">None</option></select></Field>
          <Field label="Login"><input value={f.mail_smtp_user} onChange={e => set('mail_smtp_user', e.target.value)} /></Field>
          <Field label="Password"><Secret value={f.mail_smtp_pass} onChange={e => set('mail_smtp_pass', e.target.value)} /></Field></>}
      </div>
      <div className="card-f"><Btn v="p" type="submit" ic="check" busy={busy === 'save'}>Save</Btn>{f.mail_mode !== 'smtp' && <Btn ic="mail" onClick={hostinger}>Use a Hostinger mailbox</Btn>}</div>
    </div>
    <div className="col">
      <div className="card"><Sec ic="send" c="green">Send a test</Sec><Field label="Send to"><input type="email" value={to} onChange={e => setTo(e.target.value)} placeholder="you@example.com" /></Field>
        <Btn ic="send" busy={busy === 'test'} disabled={!to} onClick={test}>Send test email</Btn></div>
      <div className="card dk c-amber pc-tip"><span className="eyebrow"><Icon n="shield" />Deliverability</span>
        <p>Server mail needs <b>SPF</b> and <b>DKIM</b> records for your domain, or it lands in spam. A real mailbox over SMTP delivers better.</p>
        <CoffeeBot w={130} /></div>
    </div>
  </form>
}

// ------------------------------------------------------------------ updates
export function Updates() {
  usePageTitle('Updates', 'Install new versions from GitHub')
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['p-upd'], queryFn: () => api.get<any>('/platform/update') })
  const [f, setF] = useState({ repo: '', channel: 'stable', token: '' })
  const [chk, setChk] = useState<any>(null)
  const [busy, setBusy] = useState('')
  useEffect(() => { if (q.data) { setF({ repo: q.data.repo, channel: q.data.channel, token: '' }); setChk(q.data.last) } }, [q.data])
  if (!q.data) return <div className="g3r"><div className="card"><Skeleton h={240} /></div><div className="card"><Skeleton h={240} /></div></div>
  const d = q.data
  const saveCfg = async (e: FormEvent) => { e.preventDefault(); await act(() => api.put('/platform/update', f), 'Saved.'); qc.invalidateQueries({ queryKey: ['p-upd'] }) }
  const check = async () => { setBusy('check'); const r = await act(() => api.post('/platform/update/check')); setBusy(''); if (r) setChk(r) }
  const apply = async () => {
    if (!confirmDo('Install ' + (chk?.tag || 'the latest version') + ' now? The server restarts for a few seconds.')) return
    setBusy('apply'); const r = await act(() => api.post('/platform/update/apply')); if (!r) { setBusy(''); return }
    toast(r.message); setTimeout(() => location.reload(), 8000)
  }
  return <div className="g3r">
    <div className="col">
      <div className="card dk c-teal pc-ver">
        <div className="pc-ver-h"><Hi n="refresh" c="teal" lg /><div className="grow"><span className="eyebrow mb0">This installation · <BrandName /></span><h2>{d.version}</h2></div>
          {chk?.available ? <Badge c="gold" dot>Update available</Badge> : chk ? <Badge c="green" dot>Up to date</Badge> : null}</div>
        <div className="pc-kv"><div><span>Commit</span><b className="mono">{d.commit || '—'}</b></div><div><span>Build</span><b>{d.asset || '—'}</b></div><div><span>Channel</span><b>{cap(d.channel || f.channel)}</b></div></div>
        <div className="row mt"><Btn ic="search" busy={busy === 'check'} onClick={check}>Check for updates</Btn>{chk?.available && d.can_self_update && <Btn v="p" ic="download" busy={busy === 'apply'} onClick={apply}>Install {chk.tag}</Btn>}</div>
      </div>
      {!d.can_self_update && <Alert t="warn">{d.self_update_note}</Alert>}
      {chk && <div className="card"><Sec ic="file" c="violet">{chk.available ? (chk.name || chk.tag) + ' is available' : 'You are up to date with ' + chk.tag}</Sec>
        {chk.published && <p className="muted sm">Published {date(chk.published)}</p>}
        {chk.notes ? <pre className="pc-notes">{chk.notes}</pre> : <p className="muted sm mb0">No release notes.</p>}</div>}
    </div>
    <form className="card" onSubmit={saveCfg}><Sec ic="sliders" c="violet">Update source</Sec>
      <Field label="GitHub repository" hint="owner/name – each of your CRM brands can follow the same repository"><input value={f.repo} onChange={e => setF({ ...f, repo: e.target.value })} /></Field>
      <Field label="Channel"><select value={f.channel} onChange={e => setF({ ...f, channel: e.target.value })}><option value="stable">Stable releases (tags like v2.1.0)</option><option value="edge">Latest commit on main (edge)</option></select></Field>
      <Field label="GitHub token" hint={d.token_set ? 'A token is saved. Enter a new one to replace it.' : 'Needed only for a private repository (fine-grained token with Contents: read).'}><Secret value={f.token} onChange={e => setF({ ...f, token: e.target.value })} placeholder={d.token_set ? '••••••••' : ''} /></Field>
      <div className="card-f"><Btn v="p" type="submit" ic="check">Save</Btn></div></form>
  </div>
}

// ------------------------------------------------------------------ platform admins
export function Admins() {
  usePageTitle('Platform admins', 'People who can manage this installation')
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['p-admins'], queryFn: () => api.get<any[]>('/platform/admins') })
  const [f, setF] = useState<{ name: string; email: string; password: string } | null>(null)
  const add = async (e: FormEvent) => { e.preventDefault(); if (await act(() => api.post('/platform/admins', f), 'Platform admin added.')) { setF(null); qc.invalidateQueries({ queryKey: ['p-admins'] }) } }
  const del = async (id: number) => { if (confirmDo('Remove this platform admin?') && await act(() => api.del('/platform/admins/' + id), 'Removed.')) qc.invalidateQueries({ queryKey: ['p-admins'] }) }
  const list = useMemo(() => q.data || [], [q.data])
  return <div className="g3r">
    <Card p0 title={<>Platform admins <Badge c="gray">{list.length}</Badge></>} ic="shield" c="violet" actions={<Btn v="p" sm ic="plus" onClick={() => setF({ name: '', email: '', password: '' })}>Add admin</Btn>}>
      {!q.data ? <Spinner /> : <div className="pc-list">{list.map(a => <div key={a.id} className="pc-li"><Avatar name={a.name} size={38} /><span className="grow"><b>{a.name}</b><small>{a.email}</small></span>
        <span className="pc-li-r"><small className="muted nowrap">Last sign-in {ago(a.last_login)}</small><button type="button" className="ib red" title="Remove" aria-label={'Remove ' + a.name} onClick={() => del(a.id)}><Icon n="trash" /></button></span></div>)}</div>}
    </Card>
    <div className="card dk c-violet pc-tip"><span className="eyebrow"><Icon n="shield" />Good to know</span>
      <p>Platform admins are never members of an organisation and organisations never see them. Use a separate email from any customer account.</p>
      <Detective w={140} /></div>
    <Modal open={!!f} onClose={() => setF(null)} title="Add platform admin" ic="shield" c="violet">{f && <form onSubmit={add}>
      <Field label="Name"><input required value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
      <Field label="Email"><input type="email" required value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
      <Field label="Password" hint="10+ characters"><Secret required minLength={10} value={f.password} onChange={e => setF({ ...f, password: e.target.value })} /></Field>
      <div className="md-f"><Btn onClick={() => setF(null)}>Cancel</Btn><Btn v="p" type="submit" ic="check">Add</Btn></div></form>}</Modal>
  </div>
}

export function Row({ children }: { children: ReactNode }) { return <div className="row">{children}</div> }
