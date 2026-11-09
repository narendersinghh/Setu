// Platform console – for the SaaS owner only (platform admins). Organisations never see it.
import { useEffect, useState } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, setCsrf } from '@/lib/api'
import { BrandMark, ThemeToggle, useBrand } from '@/lib/theme'
import { initial } from '@/lib/format'
import { Btn, Empty, Icon, Spinner, lastTitle } from '@/components/ui'
import './platform.css'
import { Admins, Branding, Mail, OrgDetail, Orgs, Overview, Plans, Updates } from './pages'

const NAV: [string, string, string, string][] = [
  ['/admin', 'Overview', 'grid', 'indigo'],
  ['/admin/orgs', 'Organisations', 'users', 'green'],
  ['/admin/plans', 'Plans & limits', 'tag', 'orange'],
  ['/admin/branding', 'Branding', 'image', 'pink'],
  ['/admin/email', 'Email', 'mail', 'blue'],
  ['/admin/updates', 'Updates', 'refresh', 'teal'],
  ['/admin/admins', 'Platform admins', 'shield', 'violet'],
]

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
  useEffect(() => { document.body.classList.remove('nav') }, [loc.pathname])
  if (me.isLoading) return <Spinner />
  if (!me.data) return <Navigate to="/login?next=/admin" replace />
  const u = me.data.user
  if (!u?.platform_admin) return <div className="ct"><Empty ic="shield" title="Platform console">This area is only for the platform owner.</Empty></div>
  if (me.data.org) { // a support session inside an organisation
    const back = async () => { await api.post('/platform/console'); location.href = '/admin' }
    return <div className="ct"><Empty ic="shield" title={'You are inside ' + me.data.org.name}><Btn v="p" ic="logout" onClick={back}>Back to the platform console</Btn></Empty></div>
  }
  const logout = async () => { await api.post('/logout').catch(() => null); location.href = '/login' }
  const cur = NAV.slice().reverse().find(n => loc.pathname.startsWith(n[0])) || NAV[0]
  return <>
    <aside className="sb" id="sb">
      <div className="sb-h brand"><BrandMark on="dark" size={38} /><span className="grow"><b>{brand.name}</b><small className="tg">Platform console</small></span></div>
      <nav><div className="ng">Platform</div>{NAV.map(([to, label, ic, c]) =>
        <NavLink key={to} to={to} end={to === '/admin'} className={({ isActive }) => 'c-' + c + (isActive ? ' on' : '')}><span className="ni"><Icon n={ic} /></span><span>{label}</span></NavLink>)}</nav>
      <div className="sb-f"><span className="me"><span className="av">{initial(u.name)}</span><span><b>{u.name}</b><small>Platform admin</small></span></span><a href="#" onClick={e => { e.preventDefault(); logout() }} title="Log out"><Icon n="logout" /></a></div>
    </aside>
    <div className="ov" onClick={() => document.body.classList.remove('nav')} />
    <main className="mn">
      <header className="tb">
        <button className="ib mob" onClick={() => document.body.classList.toggle('nav')} aria-label="Menu"><Icon n="menu" /></button>
        <span className={'hi c-' + cur[3] + ' tb-i'}><Icon n={cur[2]} /></span>
        <div className="tb-t"><h1>{head.title}</h1>{head.subtitle && <span className="sub">{head.subtitle}</span>}</div>
        <div className="tb-r"><ThemeToggle /></div>
      </header>
      <div className="ct">
        <Routes>
          <Route index element={<Overview />} />
          <Route path="orgs" element={<Orgs />} />
          <Route path="orgs/:id" element={<OrgDetail />} />
          <Route path="plans" element={<Plans />} />
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
