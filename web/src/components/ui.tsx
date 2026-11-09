// Shared UI kit – markup and class names follow the v1 design (styles/app.css).
import { ReactNode, useEffect, useState, MouseEvent, InputHTMLAttributes, useRef } from 'react'
import { Link } from 'react-router-dom'
import { ICONS } from './icons'

export function Icon({ n, c = '' }: { n: string; c?: string }) {
  return <svg className={'ic ' + c} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" dangerouslySetInnerHTML={{ __html: ICONS[n] || '' }} />
}

/** Coloured icon chip (v1 "hi"). */
export function Hi({ n, c, lg }: { n: string; c: string; lg?: boolean }) {
  return <span className={'hi c-' + c + (lg ? ' lg' : '')}><Icon n={n} /></span>
}

export function Badge({ c = 'gray', dot, children }: { c?: string; dot?: boolean; children: ReactNode }) {
  return <span className={'bdg bdg-' + c + (dot ? ' bdg-dot' : '')}>{children}</span>
}

export function Kpi({ label, value, ic, c, foot, to }: { label: string; value: ReactNode; ic: string; c: string; foot?: ReactNode; to?: string }) {
  const inner = <><span className="st-i"><Icon n={ic} /></span><span className="st-l">{label}</span><b className="st-v">{value}</b>{foot !== undefined && foot !== '' && <small className="st-f">{foot}</small>}</>
  return to ? <Link to={to} className={'stat st-' + c}>{inner}</Link> : <div className={'stat st-' + c}>{inner}</div>
}

export function Card({ title, ic, c = 'indigo', actions, children, p0, className = '' }: { title?: ReactNode; ic?: string; c?: string; actions?: ReactNode; children: ReactNode; p0?: boolean; className?: string }) {
  return <div className={'card ' + (p0 ? 'p0 ' : '') + className}>
    {title !== undefined && <div className="card-h"><h3>{ic && <Hi n={ic} c={c} />}{title}</h3>{actions && <div className="row">{actions}</div>}</div>}
    {children}
  </div>
}

/** Section heading inside a card (v1 "sec"). */
export function Sec({ ic, c, children }: { ic: string; c: string; children: ReactNode }) {
  return <div className="sec"><Hi n={ic} c={c} /><h3>{children}</h3></div>
}

type BtnProps = { v?: 'p' | 'g' | 'r' | 'y' | 'b' | 'n' | 'wa' | 'ghost' | ''; sm?: boolean; xs?: boolean; lg?: boolean; ic?: string; busy?: boolean; children?: ReactNode; onClick?: (e: MouseEvent<HTMLButtonElement>) => void; type?: 'button' | 'submit'; disabled?: boolean; title?: string; className?: string }
export function Btn({ v = '', sm, xs, lg, ic, busy, children, type = 'button', className = '', ...r }: BtnProps) {
  return <button type={type} className={['btn', v && 'btn-' + v, sm && 'btn-sm', xs && 'btn-xs', lg && 'btn-lg', className].filter(Boolean).join(' ')} disabled={busy || r.disabled} {...r}>
    {busy ? <span className="spin" /> : ic && <Icon n={ic} />}{children && <span>{children}</span>}
  </button>
}

export function IconBtn({ ic, title, onClick, bd }: { ic: string; title: string; onClick?: () => void; bd?: boolean }) {
  return <button type="button" className={'ib' + (bd ? ' bd' : '')} title={title} aria-label={title} onClick={onClick}><Icon n={ic} /></button>
}

export function Alert({ t = 'info', children }: { t?: 'ok' | 'err' | 'info' | 'warn'; children: ReactNode }) {
  return <div className={'al al-' + t}><Icon n={t === 'ok' ? 'check' : t === 'info' ? 'shield' : 'alert'} /><span>{children}</span></div>
}

export function Spinner() { return <div className="empty sm"><span className="spin" /></div> }

export function Empty({ ic = 'list', title, children }: { ic?: string; title: string; children?: ReactNode }) {
  return <div className="empty"><Hi n={ic} c="gray" lg /><h3>{title}</h3>{children && <p className="muted">{children}</p>}</div>
}

export function Modal({ open, onClose, title, ic, c = 'indigo', children, footer, size }: { open: boolean; onClose: () => void; title: ReactNode; ic?: string; c?: string; children: ReactNode; footer?: ReactNode; size?: 'lg' | 'xl' }) {
  useEffect(() => {
    if (!open) return
    const k = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    addEventListener('keydown', k)
    return () => removeEventListener('keydown', k)
  }, [open, onClose])
  if (!open) return null
  return <div className="md on" onMouseDown={e => e.target === e.currentTarget && onClose()}>
    <div className={'md-b' + (size ? ' ' + size : '')}>
      <div className="md-h"><h3>{ic && <Hi n={ic} c={c} />}{title}</h3><IconBtn ic="x" title="Close" onClick={onClose} /></div>
      {children}
      {footer && <div className="md-f">{footer}</div>}
    </div>
  </div>
}

export function Tabs<T extends string>({ tabs, value, onChange, className = '' }: { tabs: { k: T; label: ReactNode; n?: number }[]; value: T; onChange: (k: T) => void; className?: string }) {
  return <div className={'tabs ' + className}>{tabs.map(t => <a key={t.k} href="#" className={t.k === value ? 'on' : ''} onClick={e => { e.preventDefault(); onChange(t.k) }}>{t.label}{t.n !== undefined && <em>{t.n}</em>}</a>)}</div>
}

export function Pager({ page, pages, total, label = 'records', onPage }: { page: number; pages: number; total: number; label?: string; onPage: (p: number) => void }) {
  if (pages <= 1) return <div className="pg"><span className="muted">{total.toLocaleString('en-IN')} {label}</span></div>
  const out = []
  for (let i = Math.max(1, page - 3); i <= Math.min(pages, page + 3); i++) out.push(<a key={i} href="#" className={i === page ? 'on' : ''} onClick={e => { e.preventDefault(); onPage(i) }}>{i}</a>)
  return <div className="pg"><span className="muted">{total.toLocaleString('en-IN')} {label}</span>{out}</div>
}

export function Switch({ checked, onChange, disabled, title }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean; title?: string }) {
  return <label className="sw" title={title}><input type="checkbox" checked={checked} disabled={disabled} onChange={e => onChange(e.target.checked)} /><i /></label>
}

/** Labelled input row (v1 forms: <label>Text<input></label>). */
export function Field({ label, hint, children }: { label: ReactNode; hint?: ReactNode; children: ReactNode }) {
  return <label>{label}{children}{hint && <small>{hint}</small>}</label>
}

/** Password / API key input with show-hide eye. */
export function Secret(p: InputHTMLAttributes<HTMLInputElement>) {
  const [show, setShow] = useState(false)
  return <span className="pw"><input {...p} type={show ? 'text' : 'password'} autoComplete={p.autoComplete || 'new-password'} /><button type="button" className="pw-b" onClick={() => setShow(!show)} title={show ? 'Hide' : 'Show'}><Icon n="eye" /></button></span>
}

// ---- toasts
export function toast(msg: string) {
  const host = document.getElementById('toast')
  if (!host) return
  const d = document.createElement('div')
  d.textContent = msg
  host.appendChild(d)
  setTimeout(() => d.remove(), 3800)
}

/** Run an async action with a toast on success/error. */
export async function act<T>(fn: () => Promise<T>, ok?: string): Promise<T | undefined> {
  try { const r = await fn(); if (ok) toast(ok); return r } catch (e: any) { toast(e?.message || 'Something went wrong'); return undefined }
}

export function confirmDo(msg: string) { return window.confirm(msg) }

/** Latest page title (the layout may mount its listener after the page announced it). */
export let lastTitle = { title: 'Setu', subtitle: '' }

/** Page header (title & subtitle shown in the top bar). */
export function usePageTitle(title: string, subtitle = '') {
  useEffect(() => {
    document.title = title + ' · Setu'
    lastTitle = { title, subtitle }
    window.dispatchEvent(new CustomEvent('setu:title', { detail: lastTitle }))
  }, [title, subtitle])
}

/** Poll a function every ms while active. */
export function useInterval(fn: () => void, ms: number | null) {
  const ref = useRef(fn)
  ref.current = fn
  useEffect(() => {
    if (ms === null) return
    const t = setInterval(() => ref.current(), ms)
    return () => clearInterval(t)
  }, [ms])
}

/** Selection state for bulk actions in tables. */
export function useSelection<T extends number | string>() {
  const [sel, setSel] = useState<Set<T>>(new Set())
  return {
    sel, ids: [...sel],
    has: (id: T) => sel.has(id),
    toggle: (id: T) => setSel(s => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n }),
    all: (ids: T[], on: boolean) => setSel(on ? new Set(ids) : new Set()),
    clear: () => setSel(new Set()),
  }
}
