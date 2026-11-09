// Auth & onboarding: split-screen Login (+ workspace chooser), Sign up with plan picker, Forgot / Reset password.
import { FormEvent, ReactNode, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { BrandMark, ThemeToggle, useBrand } from '@/lib/theme'
import { Alert, Avatar, Btn, Field, Hi, Icon, Secret } from '@/components/ui'
import { CoffeeBot, Detective, RocketRider, Trophy } from '@/components/Cartoons'

interface Plan { id: number; name: string; slug: string; description: string; price_month: number; price_year: number; currency: string; trial_days: number; highlight: boolean; features: string[] | null; is_default?: boolean }

const FEATURES = ['Leads, pipeline board & custom fields', 'Website forms, Meta & Google Ads capture', 'Email, WhatsApp & AI calls', 'Bookings synced with Google Calendar', 'Automations that follow up for you', 'Google Maps prospecting & AI reports']

/** Brand panel (left half on desktop). */
function Side({ title, text, art }: { title: ReactNode; text: string; art?: ReactNode }) {
  const b = useBrand()
  return <aside className="auth-l" aria-hidden="false">
    <div className="al-b"><BrandMark on="dark" size={44} /><b>{b.name}</b></div>
    <div className="al-c">
      <h2>{title}</h2>
      <p>{text}</p>
      <ul>{FEATURES.map(f => <li key={f}><i><Icon n="check" /></i>{f}</li>)}</ul>
    </div>
    <div className="mock" aria-hidden="true">
      {art && <div className="mock-a">{art}</div>}
      <div className="mock-c"><div className="mk-h"><Hi n="users" c="green" /><span><b>New leads</b><small>Last 7 days</small></span></div>
        <div className="mk-v"><b>128</b><span className="trend up"><Icon n="trendUp" />+18%</span></div>
        <div className="mk-bars">{[40, 62, 35, 70, 55, 88, 100].map((h, i) => <i key={i} className={i === 6 ? 'on' : ''} style={{ height: h + '%' }} />)}</div></div>
      <div className="mock-p"><span className="bdg bdg-green bdg-dot">Won</span><span className="bdg bdg-gold bdg-dot">Proposal sent</span><span className="bdg bdg-red bdg-dot">Lost</span></div>
    </div>
    <div className="al-f">{b.tagline}</div>
  </aside>
}

/** Page frame: brand panel + form column with theme toggle. */
function Shell({ children, side, wide, top, art }: { children: ReactNode; side: [ReactNode, string]; wide?: boolean; top?: ReactNode; art?: ReactNode }) {
  const b = useBrand()
  return <div className={'auth' + (wide ? ' wide' : '')}>
    <Side title={side[0]} text={side[1]} art={art} />
    <main className="auth-r">
      <div className="auth-top"><Link to="/login" className="ab"><BrandMark size={34} />{b.name}</Link>{top}<ThemeToggle /></div>
      {art && <div className="auth-art" aria-hidden="true">{art}</div>}
      {children}
      <p className="auth-foot">© {new Date().getFullYear()} {b.name}</p>
    </main>
  </div>
}

const next = (p: URLSearchParams) => { const n = p.get('next') || '/'; return n.startsWith('/') && !n.startsWith('//') ? n : '/' }

export function Login() {
  const [p] = useSearchParams()
  const b = useBrand()
  const ws = useQuery({ queryKey: ['workspace'], queryFn: () => api.get('/workspace').catch(() => ({})) })
  const [f, setF] = useState({ email: '', password: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(0)
  const [choose, setChoose] = useState<{ id: number; name: string; role?: string }[] | null>(null)
  const go = async (org_id?: number) => {
    setBusy(org_id || -1); setErr('')
    try {
      const r = await api.post('/login', { ...f, org_id })
      if (r.choose) { setChoose(r.choose); return }
      location.href = r.console ? (next(p).startsWith('/admin') ? next(p) : '/admin') : next(p)
    } catch (e: any) { setErr(e.message) } finally { setBusy(0) }
  }
  const submit = (e: FormEvent) => { e.preventDefault(); go() }
  const signup = b.signup && ws.data?.signup !== false
  document.title = 'Sign in · ' + b.name
  return <Shell art={<RocketRider w={190} />} side={[<>Every lead, <em>one bridge</em> to conversion.</>, 'Capture, follow up and close – your whole pipeline in one calm, fast workspace.']}
    top={signup && !choose ? <span className="muted sm">New here? <Link to="/signup">Create account</Link></span> : undefined}>
    <form className="auth-f" onSubmit={submit}>
      {choose ? <>
        <h1>Choose a workspace</h1>
        <p className="muted">Your login belongs to several organisations. Pick the one to open.</p>
        {err && <Alert t="err">{err}</Alert>}
        <div className="wsl">{choose.map(w => <button key={w.id} type="button" onClick={() => go(w.id)} disabled={!!busy}><Avatar name={w.name} size={40} /><span className="grow">{w.name}{w.role && <small>{w.role === 'super' ? 'Owner' : w.role[0].toUpperCase() + w.role.slice(1)}</small>}</span>{busy === w.id ? <span className="spin" /> : <Icon n="arrowR" />}</button>)}</div>
        <p className="alt"><a href="#" onClick={e => { e.preventDefault(); setChoose(null) }}>← Use another account</a></p>
      </> : <>
        <h1>Welcome back</h1>
        <p className="muted">{ws.data?.name ? 'Sign in to ' + ws.data.name : 'Sign in to your ' + b.name + ' workspace.'}</p>
        {err && <Alert t="err">{err}</Alert>}
        <Field label="Email"><input type="email" required autoFocus value={f.email} onChange={e => setF({ ...f, email: e.target.value })} autoComplete="username" placeholder="you@company.com" /></Field>
        <label><span className="lbl-r">Password<Link to={'/forgot' + (f.email ? '?email=' + encodeURIComponent(f.email) : '')}>Forgot password?</Link></span>
          <Secret required value={f.password} onChange={e => setF({ ...f, password: e.target.value })} autoComplete="current-password" placeholder="Your password" /></label>
        <Btn v="p" lg type="submit" busy={busy === -1} ic="arrowR">Sign in</Btn>
        {signup && <p className="alt">New to {b.name}? <Link to="/signup">Create your workspace</Link></p>}
      </>}
    </form>
  </Shell>
}

function price(p: Plan, yearly: boolean) {
  const v = yearly ? p.price_year : p.price_month
  if (!v) return 'Free'
  const sym: Record<string, string> = { INR: '₹', USD: '$', EUR: '€', GBP: '£', AUD: 'A$', CAD: 'C$', SGD: 'S$', AED: 'AED ' }
  return (sym[p.currency] ?? p.currency + ' ') + v.toLocaleString('en-IN', { maximumFractionDigits: 2 })
}

export function Signup() {
  const b = useBrand()
  const [p] = useSearchParams()
  const plans = useQuery({ queryKey: ['public-plans'], queryFn: () => api.get<Plan[]>('/public/plans').catch(() => [] as Plan[]), staleTime: 300_000 })
  const list = Array.isArray(plans.data) ? plans.data : []
  const [yearly, setYearly] = useState(false)
  const [pick, setPick] = useState<number | null>(null)
  const hasYear = list.some(x => x.price_year > 0)
  const chosen = pick ?? (list.find(x => x.slug === p.get('plan')) || list.find(x => x.is_default) || list.find(x => x.highlight) || list[0])?.id ?? null
  const [f, setF] = useState({ company: '', name: '', email: '', phone: '', password: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value })
  const submit = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try { await api.post('/signup', { ...f, ...(chosen ? { plan_id: chosen } : {}) }); location.href = '/dashboard' } catch (e: any) { setErr(e.message) } finally { setBusy(false) }
  }
  document.title = 'Create account · ' + b.name
  return <Shell art={<Trophy w={180} />} wide={list.length > 1} side={[<>Start closing more deals <em>this week</em>.</>, 'Set up in minutes. Your team, leads and settings stay private to your organisation.']}
    top={<span className="muted sm">Have an account? <Link to="/login">Sign in</Link></span>}>
    <form className="auth-f" onSubmit={submit}>
      <h1>Create your workspace</h1>
      <p className="muted">{list.length ? 'Pick a plan, then tell us about you.' : 'Free to start. No credit card needed.'}</p>
      {err && <Alert t="err">{err}</Alert>}
      {list.length > 0 && <>
        {hasYear && <div className="seg per" role="group" aria-label="Billing period"><button type="button" className={!yearly ? 'on' : ''} onClick={() => setYearly(false)}>Monthly</button><button type="button" className={yearly ? 'on' : ''} onClick={() => setYearly(true)}>Yearly</button></div>}
        <div className="plans" role="radiogroup" aria-label="Plan">{list.map(x => <button key={x.id} type="button" role="radio" aria-checked={chosen === x.id} className={'plan' + (chosen === x.id ? ' on' : '')} onClick={() => setPick(x.id)}>
          {x.highlight && <span className="rb">Popular</span>}
          <span className="tick">{chosen === x.id && <Icon n="check" />}</span>
          <b>{x.name}</b>
          <span className="pr">{price(x, yearly && x.price_year > 0)}{(yearly ? x.price_year : x.price_month) > 0 && <small> / {yearly && x.price_year > 0 ? 'year' : 'month'}</small>}</span>
          {x.trial_days > 0 && <span className="bdg bdg-green sm" style={{ alignSelf: 'flex-start' }}>{x.trial_days}-day free trial</span>}
          {x.description && <p>{x.description}</p>}
          {!!x.features?.length && <ul>{x.features.slice(0, 4).map(t => <li key={t}><Icon n="check" />{t}</li>)}</ul>}
        </button>)}</div>
      </>}
      <div className="fg">
        <Field label="Company / agency name"><input required autoFocus value={f.company} onChange={set('company')} autoComplete="organization" /></Field>
        <Field label="Your name"><input required value={f.name} onChange={set('name')} autoComplete="name" /></Field>
        <Field label="Work email"><input type="email" required value={f.email} onChange={set('email')} autoComplete="username" /></Field>
        <Field label="Phone (optional)"><input value={f.phone} onChange={set('phone')} autoComplete="tel" /></Field>
      </div>
      <Field label="Password" hint={'At least 8 characters. Already have a ' + b.name + ' login? Use the same email and password.'}><Secret required minLength={8} value={f.password} onChange={set('password')} /></Field>
      <Btn v="p" lg type="submit" busy={busy} ic="rocket">Create workspace</Btn>
      <p className="alt">Already using {b.name}? <Link to="/login">Sign in</Link></p>
    </form>
  </Shell>
}

export function Forgot() {
  const b = useBrand()
  const [p] = useSearchParams()
  const [email, setEmail] = useState(p.get('email') || '')
  const [sent, setSent] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try { await api.post('/password/forgot', { email }); setSent(true) } catch (e: any) { setErr(e.message) } finally { setBusy(false) }
  }
  document.title = 'Forgot password · ' + b.name
  return <Shell art={<Detective w={180} />} side={[<>Locked out? <em>No problem.</em></>, 'We will email you a secure link to choose a new password.']}>
    <form className="auth-f" onSubmit={submit}>
      {sent ? <div className="auth-ok">
        <Hi n="mail" c="green" lg />
        <h1>Check your inbox</h1>
        <p className="muted">If an account exists for <b>{email}</b>, we have sent a link to reset the password. The link expires soon, so use it shortly.</p>
        <Link className="btn btn-lg w100" to="/login"><Icon n="arrowL" /><span>Back to sign in</span></Link>
      </div> : <>
        <h1>Forgot your password?</h1>
        <p className="muted">Enter the email you sign in with and we will send you a reset link.</p>
        {err && <Alert t="err">{err}</Alert>}
        <Field label="Email"><input type="email" required autoFocus value={email} onChange={e => setEmail(e.target.value)} autoComplete="username" placeholder="you@company.com" /></Field>
        <Btn v="p" lg type="submit" busy={busy} ic="send">Send reset link</Btn>
        <p className="alt"><Link to="/login">← Back to sign in</Link></p>
      </>}
    </form>
  </Shell>
}

export function Reset() {
  const b = useBrand()
  const [p] = useSearchParams()
  const token = p.get('token') || ''
  const [f, setF] = useState({ password: '', password2: '' })
  const [done, setDone] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault(); setErr('')
    if (f.password !== f.password2) { setErr('The two passwords do not match.'); return }
    setBusy(true)
    try { await api.post('/password/reset', { token, password: f.password }); setDone(true) } catch (e: any) { setErr(e.message) } finally { setBusy(false) }
  }
  document.title = 'Choose a new password · ' + b.name
  return <Shell art={<CoffeeBot w={180} />} side={[<>A fresh start, <em>secured</em>.</>, 'Choose a strong password you do not use anywhere else.']}>
    <form className="auth-f" onSubmit={submit}>
      {done ? <div className="auth-ok">
        <Hi n="check" c="green" lg />
        <h1>Password updated</h1>
        <p className="muted">You can now sign in with your new password.</p>
        <Link className="btn btn-p btn-lg w100" to="/login"><Icon n="arrowR" /><span>Sign in</span></Link>
      </div> : !token ? <div className="auth-ok">
        <Hi n="alert" c="amber" lg />
        <h1>Link not valid</h1>
        <p className="muted">This reset link is incomplete. Open the link from the email again, or ask for a new one.</p>
        <Link className="btn btn-p btn-lg w100" to="/forgot"><Icon n="send" /><span>Send a new link</span></Link>
      </div> : <>
        <h1>Choose a new password</h1>
        <p className="muted">At least 8 characters.</p>
        {err && <Alert t="err">{err} {/expired|invalid/i.test(err) && <Link to="/forgot">Ask for a new link</Link>}</Alert>}
        <Field label="New password"><Secret required minLength={8} autoFocus value={f.password} onChange={e => setF({ ...f, password: e.target.value })} /></Field>
        <Field label="Repeat new password"><Secret required minLength={8} value={f.password2} onChange={e => setF({ ...f, password2: e.target.value })} /></Field>
        <Btn v="p" lg type="submit" busy={busy} ic="lock">Save new password</Btn>
        <p className="alt"><Link to="/login">← Back to sign in</Link></p>
      </>}
    </form>
  </Shell>
}
