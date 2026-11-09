import { FormEvent, useState } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe, useCan, useMod, useRefreshMe } from '@/lib/session'
import { initial } from '@/lib/format'
import { useBrand } from '@/lib/theme'
import { Alert, Badge, Btn, Card, Empty, Field, Hi, Icon, Secret, Spinner, Switch, toast, usePageTitle, Sec } from '@/components/ui'
import { SettingsForm, FieldDef } from '@/components/SettingsForm'
import { SettingsTab } from '@/modules/types'
import { allSettingsTabs } from '@/modules'

// ------------------------------------------------------------------ My account
export function Account() {
  usePageTitle('My account', 'Your profile and password')
  const me = useMe(), refresh = useRefreshMe()
  const [f, setF] = useState({ name: me.user.name, email: me.user.email, phone: me.user.phone, old: '', new: '', new2: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api.put('/account', f); toast('Account updated.'); setF({ ...f, old: '', new: '', new2: '' }); refresh() } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value })
  return <>
    <div className="card hero"><div className="lh"><span className="avc" style={{ background: 'rgba(255,255,255,.15)', color: '#fff' }}>{initial(me.user.name)}</span><div className="grow"><h2>{me.user.name}</h2><div className="meta" style={{ color: '#C7CCF5' }}><span><Icon n="mail" />{me.user.email}</span><span><Icon n="shield" />{me.roles[me.user.role]?.[0]}</span><span><Icon n="grid" />{me.org.name}</span></div></div></div></div>
    <form onSubmit={save} className="g3r">
      <div className="card"><Sec ic="user" c="blue">Profile</Sec><div className="fg">
        <Field label="Name"><input required value={f.name} onChange={set('name')} /></Field>
        <Field label="Email"><input type="email" required value={f.email} onChange={set('email')} /></Field>
        <Field label="Phone"><input value={f.phone} onChange={set('phone')} /></Field></div>
        <div className="sec mt"><Hi n="shield" c="violet" /><h3>Change password <small>– optional</small></h3></div>
        <div className="fg"><Field label="New password"><Secret minLength={8} value={f.new} onChange={set('new')} /></Field><Field label="Repeat new password"><Secret minLength={8} value={f.new2} onChange={set('new2')} /></Field></div>
      </div>
      <div className="card"><Sec ic="check" c="green">Confirm</Sec><Field label="Current password" hint="Needed to save any change."><Secret required value={f.old} onChange={set('old')} autoComplete="current-password" /></Field>
        <div className="card-f"><Btn v="p" type="submit" ic="check" busy={busy}>Save changes</Btn></div></div>
    </form>
  </>
}

// ------------------------------------------------------------------ Modules
export function Modules() {
  usePageTitle('Modules', 'Switch features on or off for your whole team')
  const mod = useMod(), refresh = useRefreshMe()
  const [pro, setPro] = useState(mod('prospecting'))
  const M: [string, string, string, string, string, boolean, string[]][] = [
    ['crm', 'CRM', 'Leads, pipeline board, lead capture from forms & ads, custom fields, appointments with Google Calendar, automations, WhatsApp, email and AI calls.', 'users', 'green', true, ['Leads', 'Pipeline board', 'Appointments', 'Automations', 'Lead capture', 'Custom fields', 'Google Calendar']],
    ['prospecting', 'Prospecting', 'Google Maps scraper, website & Maps audits, AI growth reports as branded PDFs, outreach by email & WhatsApp with tracking.', 'map', 'orange', false, ['Map Scraper', 'Scraped Data', 'Audit Reports', 'AI growth reports', 'Email & WhatsApp outreach', 'Report design']],
  ]
  const save = async () => { try { await api.put('/modules', { prospecting: pro }); toast('Modules updated. Prospecting is now ' + (pro ? 'on' : 'off') + '.'); refresh() } catch (e: any) { toast(e.message) } }
  return <>
    {M.map(([k, lb, ds, ic, c, lock, tabs]) => { const on = lock || pro; return <div className="card" key={k}><div className="mod"><Hi n={ic} c={c} lg /><div className="grow">
      <h3>{lb} {lock ? <Badge>Always on</Badge> : on ? <Badge c="green" dot>On</Badge> : <Badge>Off</Badge>}</h3>
      <p className="muted sm mt0" style={{ marginTop: 4 }}>{ds}</p><ul>{tabs.map(x => <li key={x}>{x}</li>)}</ul></div>
      <Switch checked={on} disabled={lock} title={lock ? 'CRM cannot be switched off' : ''} onChange={setPro} /></div></div> })}
    <Alert>Only super admins can switch modules. When a module is off, its menu items, pages and settings are hidden for everyone; your data is kept.</Alert>
    <div className="card-f"><Btn v="p" ic="check" onClick={save}>Save modules</Btn></div>
  </>
}

// ------------------------------------------------------------------ Settings hub
const GROUPS: [SettingsTab['group'], string, string, string, string][] = [
  ['general', 'General', 'Company, brand & AI', 'grid', 'indigo'],
  ['channels', 'Messaging', 'Email, WhatsApp, calls', 'msg', 'green'],
  ['prospecting', 'Prospecting', 'Maps, audits, outreach', 'map', 'orange'],
  ['crm', 'CRM', 'Capture, fields, calendar', 'users', 'teal'],
  ['system', 'System', 'Plan, billing & API usage', 'sliders', 'violet'],
]

const TZ = ['Asia/Kolkata', 'Asia/Dubai', 'Asia/Singapore', 'Europe/London', 'America/New_York', 'America/Los_Angeles', 'Australia/Sydney', 'UTC']
const CUR: Record<string, string> = { INR: '₹', USD: '$', EUR: '€', GBP: '£', AED: 'AED ', AUD: 'A$', CAD: 'C$', SGD: 'S$', ZAR: 'R', NPR: 'Rs ', LKR: 'Rs ', BDT: '৳' }
const img: Record<string, string> = { '.png': '', '.jpg': '', '.jpeg': '', '.webp': '' }
const o = (a: string[]) => Object.fromEntries(a.map(x => [x, x]))

function CompanyTab() {
  const brand = useBrand()
  const f: FieldDef[] = [['company_name', 'Company name'], ['company_tagline', 'Tagline'], ['company_phone', 'Phone'], ['company_email', 'Email', 'email'], ['company_website', 'Website', 'url'],
    ['sender_name', 'Sender name (used in messages)'], ['company_address', 'Address', 'textarea'], ['company_about', 'About your company (shown in reports)', 'textarea'],
    ['company_logo', 'Logo for dark backgrounds (white / light logo)', 'file', img, 'Shown on dark areas: report cover, web report bar, booking page. A transparent PNG works best.'],
    ['company_logo_light', 'Logo for light backgrounds (dark / colour logo)', 'file', img, 'Shown on white areas: report closing page and emails. If you upload only one version, ' + brand.name + ' puts it on a contrasting badge so it is always readable.'],
    ['timezone', 'Timezone', 'select', o(TZ)], ['currency', 'Currency', 'select', o(Object.keys(CUR))], ['currency_symbol', 'Currency symbol', 'text', null, 'Leave empty to use the symbol of the currency.']]
  return <SettingsForm fields={f} />
}

const CORE_TABS: SettingsTab[] = [{ key: 'company', group: 'general', label: 'Company', ic: 'grid', order: 0, component: CompanyTab }]

export function Settings() {
  usePageTitle('Settings')
  const { tab = 'company' } = useParams()
  const mod = useMod()
  const tabs = [...CORE_TABS, ...allSettingsTabs].sort((a, b) => (a.order ?? 50) - (b.order ?? 50))
  const cur = tabs.find(t => t.key === tab)
  if (!cur) return <Navigate to="/settings/company" replace />
  const grp = cur.group
  const inGroup = tabs.filter(t => t.group === grp)
  const C = cur.component
  return <>
    <div className="mtabs">{GROUPS.map(([g, lb, ds, ic, c]) => { const first = tabs.find(t => t.group === g); if (!first) return null; const off = g === 'prospecting' && !mod('prospecting'); return <Link key={g} to={'/settings/' + first.key} className={'c-' + c + (grp === g ? ' on' : '') + (off ? ' off' : '')}><span className="hi"><Icon n={ic} /></span><span>{lb}<small>{off ? 'Not in your plan' : ds}</small></span></Link> })}</div>
    {grp === 'prospecting' && !mod('prospecting') ? <Alert t="warn">Prospecting is not part of your current plan. <Link to="/billing">See plans that include it</Link>.</Alert> :
      inGroup.length === 1 ? <div className="card"><C /></div> :
        <div className="sl"><nav className={'snav c-' + GROUPS.find(g => g[0] === grp)![4]}><div className="ng2">{GROUPS.find(g => g[0] === grp)![1]}</div>{inGroup.map(t => <Link key={t.key} to={'/settings/' + t.key} className={t.key === tab ? 'on' : ''}><Icon n={t.ic} />{t.label}</Link>)}</nav>
          <div className="col"><div className="card"><Sec ic={cur.ic} c={GROUPS.find(g => g[0] === grp)![4]}>{cur.label}</Sec><C /></div></div></div>}
  </>
}

// ------------------------------------------------------------------ Platform admin (Setu operators)
export function Platform() {
  const brand = useBrand()
  usePageTitle('Organisations', 'Every workspace on this ' + brand.name + ' installation')
  const me = useMe(), qc = useQueryClient()
  const [q, setQ] = useState('')
  const r = useQuery({ queryKey: ['platform', q], queryFn: () => api.get<any[]>('/platform/orgs', { q }), enabled: me.user.platform_admin })
  if (!me.user.platform_admin) return <Navigate to="/" replace />
  const set = async (id: number, status: string) => { await api.put('/platform/orgs/' + id, { status }).catch(e => toast(e.message)); qc.invalidateQueries({ queryKey: ['platform'] }) }
  return <Card p0 title="Organisations" ic="globe" c="gray" actions={<input placeholder="Search…" value={q} onChange={e => setQ(e.target.value)} />}>
    {!r.data ? <Spinner /> : !r.data.length ? <Empty title="No organisations" /> : <div className="tw"><table className="tbl"><thead><tr><th>#</th><th>Organisation</th><th>Plan</th><th className="num">Users</th><th>Status</th><th></th></tr></thead><tbody>
      {r.data.map(x => <tr key={x.id}><td className="sn">{x.id}</td><td><b>{x.name}</b><br /><small className="muted">{x.slug}</small></td><td>{x.plan}</td><td className="num">{x.users}</td><td>{x.status === 'active' ? <Badge c="green" dot>Active</Badge> : <Badge c="red">Suspended</Badge>}</td>
        <td>{x.status === 'active' ? <Btn sm v="r" onClick={() => set(x.id, 'suspended')}>Suspend</Btn> : <Btn sm v="g" onClick={() => set(x.id, 'active')}>Activate</Btn>}</td></tr>)}
    </tbody></table></div>}
  </Card>
}

export function NotFound() {
  usePageTitle('Not found')
  return <Empty ic="alert" title="Page not found">This page does not exist. <Link className="lnk" to="/dashboard">Go to the dashboard</Link></Empty>
}

export function Denied({ msg = 'You do not have access to that page.' }: { msg?: string }) {
  return <Alert t="err">{msg}</Alert>
}

/** Route guard: permission and/or module. */
export function Guard({ perm, module, children }: { perm?: string; module?: string; children: JSX.Element }) {
  const can = useCan(), mod = useMod()
  if (perm && !can(perm)) return <Denied />
  if (module && !mod(module)) return <Denied msg={'The ' + module + ' module is not included in your plan. An owner can upgrade under Plan & billing.'} />
  return children
}
