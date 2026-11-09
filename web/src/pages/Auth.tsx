import { FormEvent, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Alert, Btn, Field, Icon, Secret } from '@/components/ui'

function Side() {
  return <div className="auth-l"><img className="logo-i lg" src="/setu-logo.png" alt="Setu" /><h2>Setu</h2>
    <p>The bridge between you and your next customer. Find businesses on Google Maps, send AI growth reports, capture leads from forms &amp; ads, and follow up automatically.</p>
    <ul>
      <li><span className="ni c-orange"><Icon n="map" /></span>Google Maps prospecting &amp; audits</li>
      <li><span className="ni c-violet"><Icon n="spark" /></span>Value-first AI reports as branded PDFs</li>
      <li><span className="ni c-green"><Icon n="users" /></span>CRM with forms, Meta &amp; Google Ads leads</li>
      <li><span className="ni c-teal"><Icon n="cal" /></span>Bookings synced with Google Calendar</li>
      <li><span className="ni c-pink"><Icon n="bot" /></span>WhatsApp, email &amp; AI calls on autopilot</li>
    </ul></div>
}

const next = (p: URLSearchParams) => { const n = p.get('next') || '/'; return n.startsWith('/') && !n.startsWith('//') ? n : '/' }

export function Login() {
  const [p] = useSearchParams()
  const ws = useQuery({ queryKey: ['workspace'], queryFn: () => api.get('/workspace') })
  const [f, setF] = useState({ email: '', password: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [choose, setChoose] = useState<{ id: number; name: string }[] | null>(null)
  const go = async (org_id?: number) => {
    setBusy(true); setErr('')
    try {
      const r = await api.post('/login', { ...f, org_id })
      if (r.choose) { setChoose(r.choose); return }
      location.href = next(p)
    } catch (e: any) { setErr(e.message) } finally { setBusy(false) }
  }
  const submit = (e: FormEvent) => { e.preventDefault(); go() }
  return <div className="auth"><Side />
    <form className="auth-r" onSubmit={submit}>
      <h1>{choose ? 'Choose a workspace' : 'Welcome back'}</h1>
      <p className="muted">{choose ? 'Your login belongs to several organisations.' : ws.data?.name ? 'Sign in to ' + ws.data.name : 'Sign in to continue'}</p>
      {err && <Alert t="err">{err}</Alert>}
      {choose ? <div className="col">{choose.map(w => <Btn key={w.id} v="g" lg ic="grid" busy={busy} onClick={() => go(w.id)}>{w.name}</Btn>)}</div> : <>
        <Field label="Email"><input type="email" required autoFocus value={f.email} onChange={e => setF({ ...f, email: e.target.value })} autoComplete="username" /></Field>
        <Field label="Password"><Secret required value={f.password} onChange={e => setF({ ...f, password: e.target.value })} autoComplete="current-password" /></Field>
        <Btn v="p" lg type="submit" busy={busy} ic="logout">Sign in</Btn>
        {ws.data?.signup && <p className="muted mt">New to Setu? <Link className="lnk" to="/signup">Create your free workspace</Link></p>}
      </>}
    </form></div>
}

export function Signup() {
  const [f, setF] = useState({ company: '', name: '', email: '', phone: '', password: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value })
  const submit = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try { await api.post('/signup', f); location.href = '/dashboard' } catch (e: any) { setErr(e.message) } finally { setBusy(false) }
  }
  return <div className="auth"><Side />
    <form className="auth-r" onSubmit={submit}>
      <h1>Create your workspace</h1>
      <p className="muted">Free to start. Your team, leads and settings stay private to your organisation.</p>
      {err && <Alert t="err">{err}</Alert>}
      <Field label="Company / agency name"><input required autoFocus value={f.company} onChange={set('company')} /></Field>
      <Field label="Your name"><input required value={f.name} onChange={set('name')} /></Field>
      <Field label="Work email"><input type="email" required value={f.email} onChange={set('email')} autoComplete="username" /></Field>
      <Field label="Phone (optional)"><input value={f.phone} onChange={set('phone')} /></Field>
      <Field label="Password" hint="At least 8 characters. Already have a Setu login? Use the same email and password."><Secret required minLength={8} value={f.password} onChange={set('password')} /></Field>
      <Btn v="p" lg type="submit" busy={busy} ic="check">Create workspace</Btn>
      <p className="muted mt">Already using Setu? <Link className="lnk" to="/login">Sign in</Link></p>
    </form></div>
}
