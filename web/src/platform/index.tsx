// Platform console – for the SaaS owner only (platform admins). Organisations never see it.
// Same shell as the organisation app (dark sidebar, sticky top bar), grouped navigation, pages in ./pages.
import { useEffect, useState } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, setCsrf } from '@/lib/api'
import { BrandMark, ThemeToggle, useBrand } from '@/lib/theme'
import { Avatar, Btn, EmptyState, Icon, Spinner, lastTitle } from '@/components/ui'
import { Detective } from '@/components/Cartoons'
import './platform.css'
import { Admins, Branding, Mail, OrgDetail, Orgs, Overview, Payments, Plans, Updates } from './pages'

type Nav = [to: string, label: string, ic: string, c: string]
const NAV: [string, Nav[]][] = [
  ['Overview', [['/admin', 'Overview', 'grid', 'indigo']]],
  ['Customers', [['/admin/orgs', 'Organisations', 'users', 'green'], ['/admin/plans', 'Plans & limits', 'tag', 'orange'], ['/admin/payments', 'Payments', 'crown', 'amber']]],
  ['Brand & system', [['/admin/branding', 'Branding', 'image', 'pink'], ['/admin/email', 'Email', 'mail', 'blue'], ['/admin/updates', 'Updates', 'refresh', 'teal'], ['/admin/admins', 'Platform admins', 'shield', 'violet']]],
]
const FLAT = NAV.flatMap(g => g[1])

export default function PlatformApp() {
  const me = useQuery({ queryKey: ['me'], queryFn: async () => { const m = await api.get<any>('/me'); setCsrf(m.csrf); return m }, retry: false })
  const brand = useBrand()
  const loc = useLocation()
  const [head, setHead] = useState({ title: 'Platform', subtitle: '' })
  useEffect(() => {
    const f = (e: Event) => setHead((e as CustomEvent).detail)
    addEventListener('setu:title', f)
    setHead(lastTitle)
    return () => removeEventListener('setu:title', f)
  }, [])
  useEffect(() => { document.body.classList.remove('nav'); scrollTo(0, 0) }, [loc.pathname])
  useEffect(() => { try { if (localStorage.getItem('setu-mini')) document.body.classList.add('mini') } catch { /* private mode */ } }, [])
  if (me.isLoading) return <Spinner />
  if (!me.data) return <Navigate to="/login?next=/admin" replace />
  const u = me.data.user
  if (!u?.platform_admin) return <div className="ct"><EmptyState art={<Detective />} title="Platform console">This area is only for the platform owner.</EmptyState></div>
  if (me.data.org) { // a support session inside an organisation
    const back = async () => { await api.post('/platform/console'); location.href = '/admin' }
    return <div className="ct"><EmptyState art={<Detective />} title={'You are inside ' + me.data.org.name} action={<Btn v="p" ic="logout" onClick={back}>Back to the platform console</Btn>}>Leave the support session to manage the platform.</EmptyState></div>
  }
  const logout = async () => { await api.post('/logout').catch(() => null); location.href = '/login' }
  const cur = FLAT.slice().reverse().find(n => n[0] === '/admin' ? loc.pathname === '/admin' || loc.pathname === '/admin/' : loc.pathname.startsWith(n[0])) || FLAT[0]
  const menu = () => {
    if (innerWidth <= 900) { document.body.classList.toggle('nav'); return }
    const m = document.body.classList.toggle('mini')
    try { localStorage.setItem('setu-mini', m ? '1' : '') } catch { /* ignore */ }
  }
  return <>
    <aside className="sb pc-sb" id="sb" aria-label="Platform navigation">
      <NavLink to="/admin" className="sb-h brand" title={brand.name}><BrandMark on="dark" size={40} /><span className="grow"><b>{brand.name}</b><small className="tg">Platform console</small></span></NavLink>
      <nav>{NAV.map(([g, its]) => <div key={g}><div className="ng">{g}</div>{its.map(([to, label, ic, c]) =>
        <NavLink key={to} to={to} end={to === '/admin'} title={label} className={({ isActive }) => 'c-' + c + (isActive ? ' on' : '')}><span className="ni"><Icon n={ic} /></span><span>{label}</span></NavLink>)}</div>)}
        <span className="sb-gap" />
      </nav>
      <div className="sb-f"><span className="me"><Avatar name={u.name} size={34} /><span><b>{u.name}</b><small>Platform admin</small></span></span><button type="button" onClick={logout} title="Log out" aria-label="Log out"><Icon n="logout" /></button></div>
    </aside>
    <div className="ov" onClick={() => document.body.classList.remove('nav')} />
    <main className="mn">
      <header className="tb">
        <button className="ib bd" onClick={menu} aria-label="Show or hide the menu" title="Show / hide menu"><Icon n="sidebar" /></button>
        <span className={'hi c-' + cur[3] + ' tb-i'}><Icon n={cur[2]} /></span>
        <div className="tb-t"><h1>{head.title}</h1>{head.subtitle && <span className="sub">{head.subtitle}</span>}</div>
        <div className="tb-r"><span className="pc-env"><Icon n="globe" />{location.host}</span><ThemeToggle /></div>
      </header>
      <div className="ct">
        <Routes>
          <Route index element={<Overview />} />
          <Route path="orgs" element={<Orgs />} />
          <Route path="orgs/:id" element={<OrgDetail />} />
          <Route path="plans" element={<Plans />} />
          <Route path="payments" element={<Payments />} />
          <Route path="branding" element={<Branding />} />
          <Route path="email" element={<Mail />} />
          <Route path="updates" element={<Updates />} />
          <Route path="admins" element={<Admins />} />
          <Route path="*" element={<Navigate to="/admin" replace />} />
        </Routes>
      </div>
    </main>
  </>
}
