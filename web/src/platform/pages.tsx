import { FormEvent, ReactNode, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ago, date, num } from '@/lib/format'
import { Alert, Badge, Btn, Card, Empty, Field, Kpi, Modal, Secret, Sec, Spinner, Switch, act, confirmDo, toast, usePageTitle } from '@/components/ui'

type LimitKey = [string, string]
const statusBadge = (s: string) => s === 'active' ? <Badge c="green" dot>Active</Badge> : s === 'suspended' ? <Badge c="red">Suspended</Badge> : <Badge>{s}</Badge>
const dateInput = (v?: string | null) => (v ? v.slice(0, 10) : '')
const toISO = (v: string) => (v ? new Date(v + 'T23:59:59').toISOString() : null)
const price = (n: number, cur: string) => (n ? (cur === 'INR' ? '₹' : cur + ' ') + num(n) : 'Free')

// ------------------------------------------------------------------ overview
export function Overview() {
  usePageTitle('Platform overview', 'Every organisation on this installation')
  const q = useQuery({ queryKey: ['p-stats'], queryFn: () => api.get<any>('/platform/stats'), refetchInterval: 60_000 })
  if (!q.data) return <Spinner />
  const s = q.data.stats, sign: { day: string; n: number }[] = q.data.signups || []
  const mx = Math.max(1, ...sign.map(x => x.n))
  const mrr = (q.data.by_plan || []).reduce((t: number, p: any) => t + (p.mrr || 0), 0)
  return <>
    <div className="stats">
      <Kpi label="Organisations" value={num(s.orgs)} ic="grid" c="indigo" foot={s.new_month + ' new this month'} to="/admin/orgs" />
      <Kpi label="Active" value={num(s.active)} ic="check" c="green" foot={s.suspended + ' suspended · ' + s.trials + ' on trial'} />
      <Kpi label="Users" value={num(s.users)} ic="users" c="blue" foot={s.active_users + ' signed in today'} />
      <Kpi label="Monthly revenue (list price)" value={'₹' + num(mrr)} ic="target" c="orange" foot="sum of plan prices of active organisations" />
    </div>
    <div className="g2">
      <Card title="Sign-ups – last 30 days" ic="activity" c="violet">
        <div className="pc-pbar">{sign.map(x => <span key={x.day} title={x.day + ': ' + x.n} style={{ height: Math.max(4, (x.n / mx) * 120) }} />)}</div>
      </Card>
      <Card title="Organisations by plan" ic="tag" c="orange" p0>
        <div className="tw"><table className="tbl"><thead><tr><th>Plan</th><th className="num">Organisations</th><th className="num">MRR</th></tr></thead>
          <tbody>{(q.data.by_plan || []).map((p: any) => <tr key={p.plan}><td><b>{p.plan}</b></td><td className="num">{p.n}</td><td className="num">₹{num(p.mrr)}</td></tr>)}</tbody></table></div>
      </Card>
    </div>
    <Card title="Background work" ic="zap" c="teal"><p className="muted">{s.jobs_pending} jobs waiting · {s.jobs_failed} failed in the last 24 hours</p></Card>
  </>
}

// ------------------------------------------------------------------ organisations
export function Orgs() {
  usePageTitle('Organisations', 'Customers using this CRM')
  const [q, setQ] = useState(''), [status, setStatus] = useState(''), [plan, setPlan] = useState('')
  const plans = useQuery({ queryKey: ['p-plans'], queryFn: () => api.get<any>('/platform/plans') })
  const r = useQuery({ queryKey: ['p-orgs', q, status, plan], queryFn: () => api.get<any[]>('/platform/orgs', { q, status, plan }) })
  const go = useNavigate()
  return <Card p0 title="Organisations" ic="users" c="green" actions={<>
    <input placeholder="Search name or email…" value={q} onChange={e => setQ(e.target.value)} />
    <select value={status} onChange={e => setStatus(e.target.value)}><option value="">All statuses</option><option value="active">Active</option><option value="suspended">Suspended</option></select>
    <select value={plan} onChange={e => setPlan(e.target.value)}><option value="">All plans</option>{plans.data?.plans.map((p: any) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></>}>
    {!r.data ? <Spinner /> : !r.data.length ? <Empty ic="users" title="No organisations yet">Organisations appear here when customers sign up.</Empty> :
      <div className="tw"><table className="tbl"><thead><tr><th>Organisation</th><th>Owner</th><th>Plan</th><th className="num">Users</th><th>Status</th><th>Joined</th><th>Last seen</th></tr></thead>
        <tbody>{r.data.map(o => <tr key={o.id} className="pc-clk" onClick={() => go('/admin/orgs/' + o.id)}>
          <td><b>{o.name}</b><br /><small className="muted">#{o.id} · {o.slug}</small></td><td>{o.owner || '—'}</td>
          <td>{o.plan || '—'}{o.trial_ends_at && new Date(o.trial_ends_at) > new Date() && <> <Badge c="gold">Trial to {date(o.trial_ends_at)}</Badge></>}</td>
          <td className="num">{o.users}</td><td>{statusBadge(o.status)}</td><td>{date(o.created_at)}</td><td>{ago(o.last_seen)}</td></tr>)}</tbody></table></div>}
  </Card>
}

export function OrgDetail() {
  const { id } = useParams()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['p-org', id], queryFn: () => api.get<any>('/platform/orgs/' + id) })
  const plans = useQuery({ queryKey: ['p-plans'], queryFn: () => api.get<any>('/platform/plans') })
  const [f, setF] = useState<any>(null)
  usePageTitle(q.data?.org.name || 'Organisation', 'Plan, modules, limits and access')
  useEffect(() => {
    if (!q.data) return
    const o = q.data.org
    setF({ name: o.name, status: o.status, plan_id: o.plan_id || '', notes: o.notes || '', trial: dateInput(o.trial_ends_at), expires: dateInput(o.plan_expires_at),
      customMods: !!o.modules, modules: o.modules || o.plan_modules, customLimits: !!o.limits, limits: o.limits || {} })
  }, [q.data])
  if (!q.data || !f) return <Spinner />
  const o = q.data.org, keys: LimitKey[] = q.data.limit_keys, usage = q.data.usage
  const planLimits = (plans.data?.plans.find((p: any) => p.id === Number(f.plan_id))?.limits) || o.plan_limits || {}
  const save = async (e?: FormEvent) => {
    e?.preventDefault()
    await act(() => api.put('/platform/orgs/' + id, { name: f.name, status: f.status, plan_id: Number(f.plan_id) || 0, notes: f.notes, trial_ends_at: toISO(f.trial), plan_expires_at: toISO(f.expires),
      modules: f.customMods ? f.modules : null, limits: f.customLimits ? f.limits : null }), 'Saved.')
    qc.invalidateQueries({ queryKey: ['p-org', id] }); qc.invalidateQueries({ queryKey: ['p-orgs'] })
  }
  const enter = async () => { const r = await act(() => api.post('/platform/orgs/' + id + '/enter')); if (r) location.href = '/dashboard' }
  const mod = (m: string, on: boolean) => setF({ ...f, modules: on ? [...new Set([...(f.modules || []), m])] : (f.modules || []).filter((x: string) => x !== m) })
  return <form onSubmit={save}>
    <div className="row" style={{ marginBottom: 14 }}><Link className="lnk" to="/admin/orgs">← Organisations</Link><span className="grow" />
      {o.status === 'active' && <Btn ic="logout" onClick={enter}>Open workspace (support)</Btn>}<Btn v="p" type="submit" ic="check">Save</Btn></div>
    {o.status === 'active' && <Alert>Opening the workspace signs you in as its owner for support. You are not added to its team and its users never see you.</Alert>}
    <div className="g3r">
      <div className="col">
        <div className="card"><Sec ic="grid" c="indigo">Organisation</Sec><div className="fg">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Status"><select value={f.status} onChange={e => setF({ ...f, status: e.target.value })}><option value="active">Active</option><option value="suspended">Suspended (blocks sign-in)</option><option value="deleted">Deleted (hidden)</option></select></Field>
          <Field label="Plan"><select value={f.plan_id} onChange={e => setF({ ...f, plan_id: e.target.value })}>{plans.data?.plans.map((p: any) => <option key={p.id} value={p.id}>{p.name} – {price(p.price_month, p.currency)}/month</option>)}</select></Field>
          <Field label="Trial ends" hint="Empty = no trial"><input type="date" value={f.trial} onChange={e => setF({ ...f, trial: e.target.value })} /></Field>
          <Field label="Paid until" hint="For your own billing records"><input type="date" value={f.expires} onChange={e => setF({ ...f, expires: e.target.value })} /></Field>
          <Field label="Internal notes"><textarea rows={3} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
        </div></div>
        <div className="card"><Sec ic="layers" c="pink">Modules</Sec>
          <label className="row"><Switch checked={f.customMods} onChange={v => setF({ ...f, customMods: v })} /> Custom modules for this organisation (otherwise the plan decides)</label>
          <div className="fg mt">{[['crm', 'CRM – always on'], ['prospecting', 'Prospecting – Maps, audits, AI reports, outreach']].map(([k, l]) =>
            <label key={k} className="chk"><input type="checkbox" disabled={!f.customMods || k === 'crm'} checked={k === 'crm' || (f.modules || []).includes(k)} onChange={e => mod(k, e.target.checked)} /> {l}</label>)}</div>
        </div>
      </div>
      <div className="col">
        <div className="card"><Sec ic="sliders" c="orange">Limits & usage</Sec>
          <label className="row"><Switch checked={f.customLimits} onChange={v => setF({ ...f, customLimits: v })} /> Custom limits (empty = plan value, 0 = unlimited)</label>
          <div className="tw mt"><table className="tbl"><thead><tr><th>Limit</th><th className="num">Used</th><th className="num">Plan</th><th>Custom</th></tr></thead><tbody>
            {keys.map(([k, l]) => <tr key={k}><td>{l}</td><td className="num">{num(usage[k])}</td><td className="num">{planLimits[k] ? num(planLimits[k]) : '∞'}</td>
              <td><input type="number" min={0} disabled={!f.customLimits} style={{ width: 110 }} value={f.limits[k] ?? ''} placeholder="plan"
                onChange={e => { const l2 = { ...f.limits }; if (e.target.value === '') delete l2[k]; else l2[k] = Number(e.target.value); setF({ ...f, limits: l2 }) }} /></td></tr>)}
          </tbody></table></div>
        </div>
        <Card p0 title="Team" ic="users" c="blue"><div className="tw"><table className="tbl"><tbody>{q.data.members.map((m: any) => <tr key={m.id}><td><b>{m.name}</b><br /><small className="muted">{m.email}</small></td><td>{m.role === 'super' ? 'Owner' : m.role}</td><td>{m.active ? 'Active' : 'Off'}</td><td>{ago(m.last_login)}</td></tr>)}</tbody></table></div></Card>
      </div>
    </div>
  </form>
}

// ------------------------------------------------------------------ plans
interface Plan { id?: number; slug: string; name: string; description: string; price_month: number; price_year: number; currency: string; trial_days: number; features: string[]; limits: Record<string, number>; modules: string[]; public: boolean; is_default: boolean; highlight: boolean; sort: number; orgs?: number }
const blank: Plan = { slug: '', name: '', description: '', price_month: 0, price_year: 0, currency: 'INR', trial_days: 14, features: [], limits: {}, modules: ['crm'], public: true, is_default: false, highlight: false, sort: 10 }

export function Plans() {
  usePageTitle('Plans & limits', 'Pricing shown on sign up and the limits each plan enforces')
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['p-plans'], queryFn: () => api.get<any>('/platform/plans') })
  const [ed, setEd] = useState<Plan | null>(null)
  if (!q.data) return <Spinner />
  const keys: LimitKey[] = q.data.limit_keys
  const save = async (e: FormEvent) => {
    e.preventDefault(); if (!ed) return
    const r = await act(() => ed.id ? api.put('/platform/plans/' + ed.id, ed) : api.post('/platform/plans', ed), 'Plan saved.')
    if (r) { setEd(null); qc.invalidateQueries({ queryKey: ['p-plans'] }) }
  }
  const del = async (p: Plan) => { if (confirmDo('Delete the plan ' + p.name + '?') && await act(() => api.del('/platform/plans/' + p.id), 'Deleted.')) qc.invalidateQueries({ queryKey: ['p-plans'] }) }
  return <>
    <div className="row" style={{ marginBottom: 14 }}><p className="muted grow">Limits are enforced immediately. Changing a plan affects every organisation on it; per-organisation overrides live on the organisation page.</p><Btn v="p" ic="plus" onClick={() => setEd({ ...blank })}>New plan</Btn></div>
    <div className="pc-plans">{q.data.plans.map((p: Plan) => <div key={p.id} className={'card pc-plan' + (p.highlight ? ' pc-hl' : '')}>
      <div className="row"><h3 className="grow">{p.name}</h3>{p.is_default && <Badge c="blue">Default</Badge>}{!p.public && <Badge>Hidden</Badge>}</div>
      <p className="muted">{p.description}</p>
      <div className="pc-plan-p"><b>{price(p.price_month, p.currency)}</b>{p.price_month > 0 && <small>/month</small>}</div>
      {p.trial_days > 0 && <small className="muted">{p.trial_days}-day trial</small>}
      <ul className="ul">{p.features.map(x => <li key={x}>{x}</li>)}</ul>
      <div className="pc-plan-l">{keys.map(([k, l]) => <span key={k}><small>{l}</small><b>{p.limits[k] ? num(p.limits[k]) : '∞'}</b></span>)}</div>
      <div className="row mt"><small className="muted grow">{p.orgs} organisation(s) · {p.modules.join(', ')}</small><Btn sm ic="edit" onClick={() => setEd({ ...p })}>Edit</Btn><Btn sm v="ghost" ic="trash" title="Delete" onClick={() => del(p)} /></div>
    </div>)}</div>
    <Modal open={!!ed} onClose={() => setEd(null)} title={ed?.id ? 'Edit ' + ed.name : 'New plan'} ic="tag" c="orange" size="lg">
      {ed && <form onSubmit={save}><div className="fg">
        <Field label="Name"><input required value={ed.name} onChange={e => setEd({ ...ed, name: e.target.value })} /></Field>
        <Field label="Short name (URL-safe)" hint="Leave empty to create from the name"><input value={ed.slug} onChange={e => setEd({ ...ed, slug: e.target.value })} /></Field>
        <Field label="Description"><input value={ed.description} onChange={e => setEd({ ...ed, description: e.target.value })} /></Field>
        <Field label="Currency"><select value={ed.currency} onChange={e => setEd({ ...ed, currency: e.target.value })}>{['INR', 'USD', 'EUR', 'GBP', 'AED'].map(c => <option key={c}>{c}</option>)}</select></Field>
        <Field label="Price per month"><input type="number" min={0} value={ed.price_month} onChange={e => setEd({ ...ed, price_month: Number(e.target.value) })} /></Field>
        <Field label="Price per year"><input type="number" min={0} value={ed.price_year} onChange={e => setEd({ ...ed, price_year: Number(e.target.value) })} /></Field>
        <Field label="Free trial (days)"><input type="number" min={0} value={ed.trial_days} onChange={e => setEd({ ...ed, trial_days: Number(e.target.value) })} /></Field>
        <Field label="Order on the pricing page"><input type="number" value={ed.sort} onChange={e => setEd({ ...ed, sort: Number(e.target.value) })} /></Field>
        <Field label="Features shown on the card" hint="One per line"><textarea rows={4} value={ed.features.join('\n')} onChange={e => setEd({ ...ed, features: e.target.value.split('\n') })} /></Field>
      </div>
        <div className="sec mt"><h3>Modules</h3></div>
        <div className="row wrap"><label className="chk"><input type="checkbox" checked disabled /> CRM</label><label className="chk"><input type="checkbox" checked={ed.modules.includes('prospecting')} onChange={e => setEd({ ...ed, modules: e.target.checked ? ['crm', 'prospecting'] : ['crm'] })} /> Prospecting</label></div>
        <div className="sec mt"><h3>Limits <small>– empty or 0 = unlimited</small></h3></div>
        <div className="fg">{keys.map(([k, l]) => <Field key={k} label={l}><input type="number" min={0} value={ed.limits[k] || ''} placeholder="Unlimited" onChange={e => setEd({ ...ed, limits: { ...ed.limits, [k]: Number(e.target.value) || 0 } })} /></Field>)}</div>
        <div className="row wrap mt">
          <label className="row"><Switch checked={ed.public} onChange={v => setEd({ ...ed, public: v })} /> Show on sign up</label>
          <label className="row"><Switch checked={ed.is_default} onChange={v => setEd({ ...ed, is_default: v })} /> Default plan</label>
          <label className="row"><Switch checked={ed.highlight} onChange={v => setEd({ ...ed, highlight: v })} /> Highlight as popular</label>
        </div>
        <div className="md-f"><Btn onClick={() => setEd(null)}>Cancel</Btn><Btn v="p" type="submit" ic="check">Save plan</Btn></div>
      </form>}
    </Modal>
  </>
}

// ------------------------------------------------------------------ branding
const FONTS = ['Plus Jakarta Sans', 'Inter', 'Poppins', 'Manrope', 'DM Sans', 'Outfit', 'Sora', 'Urbanist', 'Figtree', 'Lexend', 'Montserrat', 'Nunito Sans', 'Roboto', 'Open Sans', 'Lato', 'Work Sans', 'Rubik', 'Space Grotesk', 'Raleway', 'Mulish']
const COLORS: [string, string][] = [['brand_primary', 'Primary (buttons, highlights)'], ['brand_accent', 'Accent (call-to-action)'], ['brand_success', 'Success'], ['brand_warning', 'Warning'], ['brand_danger', 'Danger'], ['brand_info', 'Info']]

function Upload({ k, label, value, onDone }: { k: string; label: string; value: string; onDone: (url: string) => void }) {
  const up = async (file?: File) => { if (!file) return; const r = await act(() => api.upload<{ url: string }>('/platform/files', { file, setting: k }), 'Uploaded.'); if (r) onDone(r.url) }
  return <Field label={label}><span className="row">{value && <img src={value} alt="" style={{ maxHeight: 40, maxWidth: 160, borderRadius: 8, background: k === 'brand_logo_dark' ? '#111' : '#f3f3f1', padding: 4 }} />}
    <input type="file" accept=".png,.jpg,.jpeg,.webp" onChange={e => up(e.target.files?.[0])} />{value && <Btn sm v="ghost" ic="trash" onClick={() => onDone('')}>Remove</Btn>}</span></Field>
}

export function Branding() {
  usePageTitle('Branding', 'White label: your CRM name, logo, typography and colours')
  const q = useQuery({ queryKey: ['p-brand'], queryFn: () => api.get<any>('/platform/brand') })
  const [f, setF] = useState<Record<string, string> | null>(null)
  useEffect(() => {
    if (!q.data) return
    const b = q.data
    setF({ brand_name: b.name, brand_tagline: b.tagline, brand_logo: b.logo, brand_logo_dark: b.logo_dark, brand_favicon: b.favicon, brand_font_heading: b.font_heading, brand_font_body: b.font_body,
      brand_radius: String(b.radius), brand_theme: b.theme_default, brand_support_email: b.support_email || '', brand_website: b.website || '',
      brand_primary: b.colors.primary, brand_accent: b.colors.accent, brand_success: b.colors.success, brand_warning: b.colors.warning, brand_danger: b.colors.danger, brand_info: b.colors.info })
  }, [q.data])
  if (!f) return <Spinner />
  const set = (k: string, v: string) => setF({ ...f, [k]: v })
  const save = async (e: FormEvent) => { e.preventDefault(); if (await act(() => api.put('/platform/brand', f), 'Branding saved – reloading to apply.')) setTimeout(() => location.reload(), 600) }
  const reset = () => setF({ ...f, brand_primary: '#C21F3A', brand_accent: '#FFD60A', brand_success: '#6BA539', brand_warning: '#F59E0B', brand_danger: '#E5484D', brand_info: '#3B82F6', brand_font_heading: 'Plus Jakarta Sans', brand_font_body: 'Inter', brand_radius: '16' })
  const pv = { '--bp': f.brand_primary, '--ba': f.brand_accent, borderRadius: Number(f.brand_radius) + 'px', fontFamily: `'${f.brand_font_body}', sans-serif` } as any
  return <form onSubmit={save} className="g3r">
    <div className="col">
      <div className="card"><Sec ic="grid" c="indigo">Identity</Sec><div className="fg">
        <Field label="CRM name" hint="Shown everywhere: sign-in, menus, emails, browser tab"><input required value={f.brand_name} onChange={e => set('brand_name', e.target.value)} /></Field>
        <Field label="Tagline"><input value={f.brand_tagline} onChange={e => set('brand_tagline', e.target.value)} /></Field>
        <Field label="Support email"><input type="email" value={f.brand_support_email} onChange={e => set('brand_support_email', e.target.value)} /></Field>
        <Field label="Website"><input type="url" value={f.brand_website} onChange={e => set('brand_website', e.target.value)} /></Field>
        <Upload k="brand_logo" label="Logo for light backgrounds" value={f.brand_logo} onDone={u => set('brand_logo', u)} />
        <Upload k="brand_logo_dark" label="Logo for dark backgrounds" value={f.brand_logo_dark} onDone={u => set('brand_logo_dark', u)} />
        <Upload k="brand_favicon" label="Favicon (square PNG)" value={f.brand_favicon} onDone={u => set('brand_favicon', u)} />
      </div></div>
      <div className="card"><Sec ic="edit" c="violet">Typography & shape</Sec><div className="fg">
        <Field label="Heading font"><select value={f.brand_font_heading} onChange={e => set('brand_font_heading', e.target.value)}>{FONTS.map(x => <option key={x}>{x}</option>)}</select></Field>
        <Field label="Body font"><select value={f.brand_font_body} onChange={e => set('brand_font_body', e.target.value)}>{FONTS.map(x => <option key={x}>{x}</option>)}</select></Field>
        <Field label={'Corner radius: ' + f.brand_radius + 'px'}><input type="range" min={4} max={28} value={f.brand_radius} onChange={e => set('brand_radius', e.target.value)} /></Field>
        <Field label="Default theme"><select value={f.brand_theme} onChange={e => set('brand_theme', e.target.value)}><option value="system">Follow the device</option><option value="light">Light</option><option value="dark">Dark</option></select></Field>
      </div></div>
    </div>
    <div className="col">
      <div className="card"><Sec ic="image" c="pink">Colours</Sec><div className="fg">
        {COLORS.map(([k, l]) => <Field key={k} label={l}><span className="row"><input type="color" value={f[k]} onChange={e => set(k, e.target.value)} /><input value={f[k]} onChange={e => set(k, e.target.value)} style={{ width: 110 }} /></span></Field>)}
      </div><Btn sm v="ghost" ic="refresh" onClick={reset}>Reset to default palette</Btn></div>
      <div className="card"><Sec ic="eye" c="teal">Preview</Sec>
        <div className="pc-bprev" style={pv}><h3 style={{ fontFamily: `'${f.brand_font_heading}', sans-serif` }}>{f.brand_name}</h3><p>{f.brand_tagline}</p>
          <div className="row"><span className="pc-bprev-b" style={{ background: f.brand_primary }}>Primary</span><span className="pc-bprev-b" style={{ background: f.brand_accent, color: '#111' }}>Confirm order</span><span className="pc-bprev-b" style={{ background: f.brand_success }}>Delivered</span><span className="pc-bprev-b" style={{ background: f.brand_danger }}>Canceled</span></div></div>
      </div>
      <div className="card-f"><Btn v="p" type="submit" ic="check">Save branding</Btn></div>
    </div>
  </form>
}

// ------------------------------------------------------------------ email (platform mailer)
export function Mail() {
  usePageTitle('Email', 'Default mailer for every organisation without its own email set-up')
  const q = useQuery({ queryKey: ['p-mail'], queryFn: () => api.get<Record<string, string>>('/platform/mail') })
  const [f, setF] = useState<Record<string, string> | null>(null)
  const [to, setTo] = useState('')
  useEffect(() => { if (q.data) setF(q.data) }, [q.data])
  if (!f) return <Spinner />
  const set = (k: string, v: string) => setF({ ...f, [k]: v })
  const save = async (e: FormEvent) => { e.preventDefault(); await act(() => api.put('/platform/mail', f), 'Email settings saved.') }
  const test = async () => { const r = await act(() => api.post('/platform/mail/test', { to })); if (r) toast(r.message) }
  const hostinger = () => setF({ ...f, mail_mode: 'smtp', mail_smtp_host: 'smtp.hostinger.com', mail_smtp_port: '465', mail_smtp_secure: 'ssl' })
  return <form onSubmit={save} className="g3r">
    <div className="card"><Sec ic="mail" c="blue">Default mailer</Sec>
      <Alert>Organisations send with this mailer until they connect their own SMTP mailbox or email service in Settings → Email. Emails show the organisation's name and replies go to the organisation's email.</Alert>
      <div className="fg">
        <Field label="How to send"><select value={f.mail_mode} onChange={e => set('mail_mode', e.target.value)}><option value="sendmail">Server mail (sendmail / Postfix – like PHP mail)</option><option value="smtp">SMTP mailbox (recommended for delivery)</option></select></Field>
        <Field label="From email" hint="An address on your own domain, e.g. no-reply@yourdomain.com"><input type="email" value={f.mail_from_email} onChange={e => set('mail_from_email', e.target.value)} /></Field>
        <Field label="From name (fallback)"><input value={f.mail_from_name} onChange={e => set('mail_from_name', e.target.value)} /></Field>
        {f.mail_mode === 'sendmail' ? <Field label="sendmail path"><input value={f.mail_sendmail_path} onChange={e => set('mail_sendmail_path', e.target.value)} /></Field> : <>
          <Field label="SMTP server"><input value={f.mail_smtp_host} onChange={e => set('mail_smtp_host', e.target.value)} placeholder="smtp.hostinger.com" /></Field>
          <Field label="Port"><input value={f.mail_smtp_port} onChange={e => set('mail_smtp_port', e.target.value)} /></Field>
          <Field label="Encryption"><select value={f.mail_smtp_secure} onChange={e => set('mail_smtp_secure', e.target.value)}><option value="ssl">SSL (465)</option><option value="tls">STARTTLS (587)</option><option value="none">None</option></select></Field>
          <Field label="Login"><input value={f.mail_smtp_user} onChange={e => set('mail_smtp_user', e.target.value)} /></Field>
          <Field label="Password"><Secret value={f.mail_smtp_pass} onChange={e => set('mail_smtp_pass', e.target.value)} /></Field></>}
      </div>
      <div className="card-f"><Btn v="p" type="submit" ic="check">Save</Btn>{f.mail_mode !== 'smtp' && <Btn ic="mail" onClick={hostinger}>Use a Hostinger mailbox</Btn>}</div>
    </div>
    <div className="card"><Sec ic="send" c="green">Send a test</Sec><Field label="Send to"><input type="email" value={to} onChange={e => setTo(e.target.value)} placeholder="you@example.com" /></Field>
      <div className="card-f"><Btn ic="send" onClick={test}>Send test email</Btn></div>
      <p className="hint">Server mail needs SPF and DKIM records for your domain or it lands in spam. A real mailbox over SMTP delivers better.</p></div>
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
  if (!q.data) return <Spinner />
  const d = q.data
  const saveCfg = async (e: FormEvent) => { e.preventDefault(); await act(() => api.put('/platform/update', f), 'Saved.'); qc.invalidateQueries({ queryKey: ['p-upd'] }) }
  const check = async () => { setBusy('check'); const r = await act(() => api.post('/platform/update/check')); setBusy(''); if (r) setChk(r) }
  const apply = async () => {
    if (!confirmDo('Install ' + (chk?.tag || 'the latest version') + ' now? Setu restarts for a few seconds.')) return
    setBusy('apply'); const r = await act(() => api.post('/platform/update/apply')); if (!r) { setBusy(''); return }
    toast(r.message); setTimeout(() => location.reload(), 8000)
  }
  return <div className="g3r">
    <div className="col">
      <div className="card"><Sec ic="refresh" c="teal">This installation</Sec>
        <div className="pc-kv"><span>Version</span><b>{d.version}</b><span>Commit</span><b>{d.commit || '—'}</b><span>Build</span><b>{d.asset}</b></div>
        {!d.can_self_update && <Alert t="warn">{d.self_update_note}</Alert>}
        {chk && <div className="mt">{chk.available ? <Alert t="ok"><b>{chk.name || chk.tag}</b> is available (published {date(chk.published)}).</Alert> : <Alert>You are up to date with {chk.tag}.</Alert>}
          {chk.notes && <pre className="pc-notes">{chk.notes}</pre>}</div>}
        <div className="card-f"><Btn ic="search" busy={busy === 'check'} onClick={check}>Check for updates</Btn>{chk?.available && d.can_self_update && <Btn v="p" ic="download" busy={busy === 'apply'} onClick={apply}>Install update</Btn>}</div>
      </div>
    </div>
    <form className="card" onSubmit={saveCfg}><Sec ic="sliders" c="violet">Update source</Sec><div className="fg">
      <Field label="GitHub repository" hint="owner/name – each of your CRM brands can follow the same repository"><input value={f.repo} onChange={e => setF({ ...f, repo: e.target.value })} /></Field>
      <Field label="Channel"><select value={f.channel} onChange={e => setF({ ...f, channel: e.target.value })}><option value="stable">Stable releases (tags like v2.1.0)</option><option value="edge">Latest commit on main (edge)</option></select></Field>
      <Field label="GitHub token" hint={d.token_set ? 'A token is saved. Enter a new one to replace it.' : 'Needed only for a private repository (fine-grained token with Contents: read).'}><Secret value={f.token} onChange={e => setF({ ...f, token: e.target.value })} placeholder={d.token_set ? '••••••••' : ''} /></Field>
    </div><div className="card-f"><Btn v="p" type="submit" ic="check">Save</Btn></div></form>
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
  return <>
    <Card p0 title="Platform admins" ic="shield" c="violet" actions={<Btn v="p" sm ic="plus" onClick={() => setF({ name: '', email: '', password: '' })}>Add admin</Btn>}>
      {!q.data ? <Spinner /> : <div className="tw"><table className="tbl"><tbody>{q.data.map(a => <tr key={a.id}><td><b>{a.name}</b><br /><small className="muted">{a.email}</small></td><td>last sign-in {ago(a.last_login)}</td><td><Btn sm v="ghost" ic="trash" title="Remove" onClick={() => del(a.id)} /></td></tr>)}</tbody></table></div>}
    </Card>
    <Alert>Platform admins are never members of an organisation and organisations never see them. Use a separate email from any customer account.</Alert>
    <Modal open={!!f} onClose={() => setF(null)} title="Add platform admin" ic="shield" c="violet">{f && <form onSubmit={add}><div className="fg">
      <Field label="Name"><input required value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
      <Field label="Email"><input type="email" required value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
      <Field label="Password" hint="10+ characters"><Secret required minLength={10} value={f.password} onChange={e => setF({ ...f, password: e.target.value })} /></Field>
    </div><div className="md-f"><Btn onClick={() => setF(null)}>Cancel</Btn><Btn v="p" type="submit" ic="check">Add</Btn></div></form>}</Modal>
  </>
}

export function Row({ children }: { children: ReactNode }) { return <div className="row">{children}</div> }
