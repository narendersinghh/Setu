// Signed-in app shell: dark sidebar (brand, workspace, grouped nav, plan card), top bar (title, search ⌘K,
// theme, usage, activity, Create menu, account menu) and the activity drawer.
import { useEffect, useRef, useState, FormEvent, lazy, Suspense } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe, useCan, useMod, planName, Limit } from '@/lib/session'
import { BrandMark, ThemeToggle, useBrand } from '@/lib/theme'
import { Avatar, Icon, lastTitle } from './ui'

const ActivityDrawer = lazy(() => import('@/modules/reports/ActivityDrawer'))

type Item = { to: string; label: string; ic: string; c: string; n?: number; match?: string[] }

export function useNav() {
  const can = useCan(), mod = useMod()
  const pro = mod('prospecting') && can('prospecting')
  const { data: n = {} as Record<string, number | undefined> } = useQuery({ queryKey: ['nav'], queryFn: () => api.get<Record<string, number>>('/nav').catch(() => ({} as Record<string, number>)), refetchInterval: 60_000 })
  const g: [string, Item[]][] = [['Overview', [{ to: '/dashboard', label: 'Dashboard', ic: 'grid', c: 'indigo', match: ['/activity'] }]]]
  if (pro) g.push(['Prospecting', [
    { to: '/scraper', label: 'Map Scraper', ic: 'map', c: 'orange', match: ['/campaigns'] },
    { to: '/prospects', label: 'Scraped Data', ic: 'db', c: 'sky', n: n.scraped },
    { to: '/reports', label: 'Audit Reports', ic: 'file', c: 'violet' },
  ]])
  const crm: Item[] = [
    { to: '/leads', label: 'Leads', ic: 'users', c: 'green', n: n.leads_new },
    { to: '/emails', label: 'Emails', ic: 'mail', c: 'blue', n: n.mail_unread },
    { to: '/appointments', label: 'Appointments', ic: 'cal', c: 'teal', n: n.appt_today },
  ]
  if (can('automations')) crm.push({ to: '/automations', label: 'Automations', ic: 'bot', c: 'pink' })
  g.push(['CRM', crm])
  if (can('settings')) g.push(['Setup', [{ to: '/settings', label: 'Settings', ic: 'sliders', c: 'violet', match: ['/usage'] }, { to: '/team', label: 'Team & Roles', ic: 'shield', c: 'blue' }]])
  return g
}

/** Open/close state for a dropdown that closes on outside click and Escape. */
function useDrop() {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const down = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false) }
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('mousedown', down); document.removeEventListener('keydown', key) }
  }, [open])
  return { open, setOpen, ref, toggle: () => setOpen(o => !o) }
}

const LIMIT_LABEL: Record<string, string> = { leads: 'Leads', users: 'Users', automations: 'Automations', sources: 'Lead sources', searches_month: 'Map searches / mo', reports_month: 'Reports / mo', emails_month: 'Emails / mo', calls_month: 'AI calls / mo' }
const LIMIT_ORDER = ['leads', 'users', 'emails_month', 'automations', 'searches_month', 'reports_month', 'calls_month', 'sources']

function PlanCard() {
  const me = useMe(), can = useCan()
  const plan = planName(me.org)
  const lim = Object.entries(me.limits || {}).filter(([, v]) => v && v.limit > 0) as [string, Limit][]
  const ord = (k: string) => { const i = LIMIT_ORDER.indexOf(k); return i < 0 ? 99 : i }
  lim.sort((a, b) => ord(a[0]) - ord(b[0]))
  if (!plan && !lim.length) return null
  return <div className="sb-plan" title={plan ? plan + ' plan' : 'Your plan'}>
    <div className="pl-h"><span className="pl-i"><Icon n="crown" /></span><span className="pl-x"><b>{plan ? plan + ' plan' : 'Your plan'}</b><small>{lim.length ? 'Usage this period' : 'Thanks for using us'}</small></span></div>
    {lim.length > 0 && <div className="pl-u">{lim.slice(0, 2).map(([k, v]) => {
      const pc = Math.min(100, Math.round((v.used / v.limit) * 100))
      return <div key={k} className="pl-r"><span>{LIMIT_LABEL[k] || k}<b>{v.used.toLocaleString('en-IN')} / {v.limit.toLocaleString('en-IN')}</b></span><div className="pl-t"><i className={pc >= 90 ? 'hot' : ''} style={{ width: pc + '%' }} /></div></div>
    })}</div>}
    {can('settings') && <Link className="btn btn-p btn-sm" to="/usage"><Icon n="rocket" /><span>Plan &amp; usage</span></Link>}
  </div>
}

export default function Layout() {
  const me = useMe(), can = useCan(), mod = useMod(), brand = useBrand()
  const nav = useNav()
  const loc = useLocation(), go = useNavigate()
  const [head, setHead] = useState({ title: '', subtitle: '' })
  const [drawer, setDrawer] = useState(false)
  const create = useDrop(), user = useDrop(), ws = useDrop()
  const [q, setQ] = useState('')
  const searchRef = useRef<HTMLInputElement>(null)
  const tick = useQuery({ queryKey: ['tick'], queryFn: () => api.get<{ due: number; running: number }>('/activity/tick').catch(() => ({ due: 0, running: 0 })), enabled: can('settings'), refetchInterval: 15_000 })
  const busy = (tick.data?.due || 0) + (tick.data?.running || 0)
  useEffect(() => {
    const f = (e: Event) => setHead((e as CustomEvent).detail)
    addEventListener('setu:title', f)
    setHead(lastTitle)
    return () => removeEventListener('setu:title', f)
  }, [])
  useEffect(() => { document.body.classList.remove('nav'); create.setOpen(false); user.setOpen(false); ws.setOpen(false) }, [loc.pathname])
  useEffect(() => { try { if (localStorage.getItem('setu-mini')) document.body.classList.add('mini') } catch { /* private mode */ } }, [])
  // ⌘K / Ctrl+K (and "/") focus the search box
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      const typing = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)
      if (((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') || (e.key === '/' && !typing)) {
        if (!searchRef.current || searchRef.current.offsetParent === null) return
        e.preventDefault(); searchRef.current.focus(); searchRef.current.select()
      }
    }
    addEventListener('keydown', k)
    return () => removeEventListener('keydown', k)
  }, [])
  const active = (it: Item) => loc.pathname.startsWith(it.to) || (it.match || []).some(m => loc.pathname.startsWith(m))
  let cur: Item = { to: '', label: '', ic: 'grid', c: 'indigo' }
  nav.forEach(([, its]) => its.forEach(it => { if (active(it)) cur = it }))
  const menu = () => {
    if (innerWidth <= 900) { document.body.classList.toggle('nav'); return }
    const m = document.body.classList.toggle('mini')
    try { localStorage.setItem('setu-mini', m ? '1' : '') } catch { /* ignore */ }
  }
  const search = (e: FormEvent) => { e.preventDefault(); go((loc.pathname.startsWith('/prospects') ? '/prospects' : '/leads') + '?q=' + encodeURIComponent(q)) }
  const logout = async () => { await api.post('/logout').catch(() => null); location.href = '/login' }
  const switchTo = async (id: number) => { if (id === me.org.id) return; await api.post('/switch', { org_id: id }); location.href = '/' }
  const toConsole = async () => { await api.post('/platform/console').catch(() => null); location.href = '/admin' }
  const company = me.settings.company_name || me.org.name
  const role = me.roles[me.user.role]?.[0] || me.user.role
  const mac = typeof navigator !== 'undefined' && /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent)
  const wsList = <>{me.workspaces.map(w => <button key={w.id} type="button" className={w.id === me.org.id ? 'on' : ''} onClick={() => switchTo(w.id)}><Avatar name={w.name} size={24} /><span className="grow ell">{w.name}</span>{w.id === me.org.id && <Icon n="check" c="chk-i" />}</button>)}</>
  return <>
    <aside className="sb" id="sb" aria-label="Main navigation">
      <Link to="/" className="sb-h brand" title={brand.name}><BrandMark on="dark" size={40} /><span className="grow"><b>{brand.name}</b>{brand.tagline && <small className="tg">{brand.tagline}</small>}</span></Link>
      <div className={'sb-co dd' + (ws.open ? ' open' : '')} ref={ws.ref}>
        {me.workspaces.length > 1
          ? <button type="button" onClick={ws.toggle} aria-haspopup="menu" aria-expanded={ws.open} title={'Workspace: ' + company}><Avatar name={company} size={28} /><span className="grow"><b>{company}</b><small>{role}</small></span><Icon n="swap" /></button>
          : <div title={company}><Avatar name={company} size={28} /><span className="grow"><b>{company}</b><small>{role}</small></span></div>}
        {me.workspaces.length > 1 && <div className="ddm" role="menu"><div className="dh">Switch workspace</div>{wsList}</div>}
      </div>
      <nav>{nav.map(([g, its]) => <div key={g}><div className="ng">{g}</div>{its.map(it =>
        <NavLink key={it.to} to={it.to} title={it.label} className={() => 'c-' + it.c + (active(it) ? ' on' : '')} aria-current={active(it) ? 'page' : undefined}><span className="ni"><Icon n={it.ic} /></span><span>{it.label}</span>{!!it.n && <em>{it.n > 999 ? '999+' : it.n}</em>}</NavLink>)}</div>)}</nav>
      <PlanCard />
    </aside>
    <div className="ov" onClick={() => document.body.classList.remove('nav')} />
    <main className="mn">
      {me.support && <div className="sup-bar" role="status"><Icon n="shield" /><span>Support session – you are inside <b>{me.org.name}</b> as a platform admin. You are hidden from the team.</span><button type="button" onClick={toConsole}>Back to console</button></div>}
      <header className="tb">
        <button className="ib bd" onClick={menu} aria-label="Show or hide the menu" title="Show / hide menu"><Icon n="sidebar" /></button>
        <span className={'hi c-' + cur.c + ' tb-i'}><Icon n={cur.ic} /></span>
        <div className="tb-t"><h1>{head.title}</h1>{head.subtitle && <span className="sub">{head.subtitle}</span>}</div>
        <div className="tb-r">
          <form className="gs" onSubmit={search} role="search"><Icon n="search" /><input ref={searchRef} aria-label="Search leads" placeholder="Search leads, phone, email…" value={q} onChange={e => setQ(e.target.value)} /><kbd>{mac ? '⌘' : 'Ctrl'} K</kbd></form>
          <ThemeToggle />
          {can('settings') && <><Link className="ib bd act-b usage-b" to="/usage" title="API usage & costs" aria-label="API usage and costs"><Icon n="db" /></Link><button className={'ib bd act-b' + (busy > 0 ? ' run' : '')} onClick={() => setDrawer(true)} title="Activity & processes" aria-label={'Activity and processes' + (busy ? ', ' + busy + ' running' : '')}><Icon n="activity" />{busy > 0 && <span className="badge">{busy}</span>}</button></>}
          <div className={'dd' + (create.open ? ' open' : '')} ref={create.ref}><button className="btn btn-p" onClick={create.toggle} aria-haspopup="menu" aria-expanded={create.open} title="Create"><Icon n="plus" /><span>Create</span></button>
            <div className="ddm" role="menu"><div className="dh">Create new</div><Link to="/leads/new"><Icon n="user" />Lead</Link><Link to="/appointments?add=1"><Icon n="cal" />Appointment</Link>{mod('prospecting') && can('prospecting') && <Link to="/scraper?new=1"><Icon n="map" />Map search</Link>}{can('automations') && <Link to="/automations/new"><Icon n="bot" />Automation</Link>}</div>
          </div>
          <div className={'dd' + (user.open ? ' open' : '')} ref={user.ref}>
            <button className="tb-me" onClick={user.toggle} aria-haspopup="menu" aria-expanded={user.open} aria-label="Account menu"><Avatar name={me.user.name} size={34} /><span>{me.user.name.split(' ')[0]}</span><Icon n="chev" /></button>
            <div className="ddm" role="menu" style={{ minWidth: 250 }}>
              <div className="ddm-u"><Avatar name={me.user.name} size={38} /><span className="grow"><b>{me.user.name}</b><small>{me.user.email}</small></span></div>
              <Link to="/account"><Icon n="user" />My account</Link>
              {can('team') && <Link to="/team"><Icon n="shield" />Team &amp; roles</Link>}
              {me.workspaces.length > 1 && <><div className="dh">Switch workspace</div>{wsList}</>}
              {me.user.platform_admin && <button type="button" onClick={toConsole}><Icon n="globe" />Platform console</button>}
              <hr /><button type="button" className="red" onClick={logout}><Icon n="logout" />Log out</button>
            </div>
          </div>
        </div>
      </header>
      <div className="ct"><Outlet /></div>
    </main>
    {can('settings') && drawer && <Suspense fallback={null}><ActivityDrawer onClose={() => setDrawer(false)} /></Suspense>}
  </>
}
