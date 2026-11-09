import { useEffect, useState, FormEvent, lazy, Suspense } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe, useCan, useMod } from '@/lib/session'
import { initial } from '@/lib/format'
import { Icon, lastTitle } from './ui'

const ActivityDrawer = lazy(() => import('@/modules/reports/ActivityDrawer'))

type Item = { to: string; label: string; ic: string; c: string; n?: number; match?: string[] }

export function useNav() {
  const me = useMe(), can = useCan(), mod = useMod()
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
  if (can('settings')) {
    const s: Item[] = [{ to: '/settings', label: 'Settings', ic: 'sliders', c: 'violet', match: ['/usage'] }, { to: '/team', label: 'Team & Roles', ic: 'shield', c: 'blue' }]
    if (can('modules')) s.push({ to: '/modules', label: 'Modules', ic: 'layers', c: 'pink' })
    g.push(['Setup', s])
  }
  if (me.user.platform_admin) g.push(['Platform', [{ to: '/platform', label: 'Organisations', ic: 'globe', c: 'gray' }]])
  return g
}

export default function Layout() {
  const me = useMe(), can = useCan(), mod = useMod()
  const nav = useNav()
  const loc = useLocation(), go = useNavigate()
  const [head, setHead] = useState({ title: 'Setu', subtitle: '' })
  const [drawer, setDrawer] = useState(false)
  const [create, setCreate] = useState(false)
  const [q, setQ] = useState('')
  const tick = useQuery({ queryKey: ['tick'], queryFn: () => api.get<{ due: number; running: number }>('/activity/tick').catch(() => ({ due: 0, running: 0 })), enabled: can('settings'), refetchInterval: 15_000 })
  const busy = (tick.data?.due || 0) + (tick.data?.running || 0)
  useEffect(() => {
    const f = (e: Event) => setHead((e as CustomEvent).detail)
    addEventListener('setu:title', f)
    setHead(lastTitle)
    return () => removeEventListener('setu:title', f)
  }, [])
  useEffect(() => { document.body.classList.remove('nav'); setCreate(false) }, [loc.pathname])
  useEffect(() => { try { if (localStorage.getItem('setu-mini')) document.body.classList.add('mini') } catch { /* private mode */ } }, [])
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
  const switchTo = async (id: number) => { await api.post('/switch', { org_id: id }); location.href = '/' }
  const company = me.settings.company_name || me.org.name
  return <>
    <aside className="sb" id="sb">
      <Link to="/" className="sb-h brand"><img className="logo-i" src="/setu-logo.png" alt="Setu" /><span className="grow"><b>Setu</b><small className="tg">Bridge Every Lead to Conversion</small></span></Link>
      <div className="sb-co" title={company}><Icon n="grid" /><span>{company}</span>
        {me.workspaces.length > 1 && <select className="ws-sw" value={me.org.id} onChange={e => switchTo(Number(e.target.value))} title="Switch workspace">{me.workspaces.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select>}
      </div>
      <nav>{nav.map(([g, its]) => <div key={g}><div className="ng">{g}</div>{its.map(it =>
        <NavLink key={it.to} to={it.to} className={() => 'c-' + it.c + (active(it) ? ' on' : '')}><span className="ni"><Icon n={it.ic} /></span><span>{it.label}</span>{!!it.n && <em>{it.n > 999 ? '999+' : it.n}</em>}</NavLink>)}</div>)}</nav>
      <div className="sb-f"><Link to="/account" className="me" title="My account"><span className="av">{initial(me.user.name)}</span><span><b>{me.user.name}</b><small>{me.roles[me.user.role]?.[0]} · v2</small></span></Link><a href="#" onClick={e => { e.preventDefault(); logout() }} title="Log out"><Icon n="logout" /></a></div>
    </aside>
    <div className="ov" onClick={() => document.body.classList.remove('nav')} />
    <main className="mn">
      <header className="tb">
        <button className="ib mob" onClick={menu} aria-label="Menu" title="Show / hide menu"><Icon n="menu" /></button>
        <span className={'hi c-' + cur.c + ' tb-i'}><Icon n={cur.ic} /></span>
        <div className="tb-t"><h1>{head.title}</h1>{head.subtitle && <span className="sub">{head.subtitle}</span>}</div>
        <div className="tb-r">
          <form className="gs" onSubmit={search}><Icon n="search" /><input placeholder="Search leads, phone, email…" value={q} onChange={e => setQ(e.target.value)} /></form>
          {can('settings') && <><Link className="ib bd act-b" to="/usage" title="API usage & costs"><Icon n="db" /></Link><button className="ib bd act-b" onClick={() => setDrawer(true)} title="Activity & processes"><Icon n="activity" />{busy > 0 && <span className="badge">{busy}</span>}</button></>}
          <div className={'dd' + (create ? ' open' : '')}><button className="btn btn-p" onClick={() => setCreate(!create)}><Icon n="plus" /><span>Create</span></button>
            <div className="ddm"><Link to="/leads/new"><Icon n="user" />New lead</Link><Link to="/appointments?add=1"><Icon n="cal" />Appointment</Link>{mod('prospecting') && can('prospecting') && <Link to="/scraper?new=1"><Icon n="map" />Map search</Link>}{can('automations') && <Link to="/automations/new"><Icon n="bot" />Automation</Link>}</div>
          </div>
        </div>
      </header>
      <div className="ct"><Outlet /></div>
    </main>
    {can('settings') && drawer && <Suspense fallback={null}><ActivityDrawer onClose={() => setDrawer(false)} /></Suspense>}
  </>
}
