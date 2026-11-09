// Shared UI kit – v1 markup and class names (styles/app.css) restyled by the v2 design tokens,
// plus v2 components: Avatar, Trend, Progress, Donut, AreaChart, Skeleton, EmptyState.
import { ReactNode, useEffect, useState, MouseEvent, InputHTMLAttributes, useRef, useId, useMemo, PointerEvent } from 'react'
import { Link } from 'react-router-dom'
import { ICONS } from './icons'
import { brandName } from '@/lib/theme'

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
  return <div className="empty"><Hi n={ic} c="indigo" lg /><h3>{title}</h3>{children && <p className="muted">{children}</p>}</div>
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
export let lastTitle = { title: '', subtitle: '' }

/** Page header (title & subtitle shown in the top bar). */
export function usePageTitle(title: string, subtitle = '') {
  useEffect(() => {
    document.title = title + ' · ' + brandName
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

// ================================================================== v2 components

const AVC = ['indigo', 'violet', 'blue', 'sky', 'teal', 'green', 'amber', 'orange', 'rose', 'pink']
/** Stable colour family for a name. */
export function nameColor(s: string) { let h = 0; for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0; return AVC[h % AVC.length] }

/** Round avatar with initials (or an image). */
export function Avatar({ name, src, size = 36, c }: { name: string; src?: string; size?: number; c?: string }) {
  const parts = (name || '?').trim().split(/\s+/)
  const ini = ((parts[0]?.[0] || '?') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase()
  const st = { width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.38)) }
  if (src) return <img className="avt" src={src} alt="" style={st} />
  return <span className={'avt c-' + (c || nameColor(name || '?'))} style={st} aria-hidden="true">{ini}</span>
}

/** Trend pill: +12% (green), -3% (red), 0 (grey). */
export function Trend({ v, suffix = '%', label }: { v: number; suffix?: string; label?: ReactNode }) {
  const k = v > 0 ? 'up' : v < 0 ? 'dn' : 'flat'
  return <span className={'trend ' + k}>{k !== 'flat' && <Icon n={k === 'up' ? 'trendUp' : 'trendDn'} />}{v > 0 ? '+' : ''}{Number.isFinite(v) ? v : 0}{suffix}{label && <em>{label}</em>}</span>
}

/** Labelled progress bar. c = colour family. */
export function Progress({ value, max = 100, c = 'indigo', label, sub }: { value: number; max?: number; c?: string; label?: ReactNode; sub?: ReactNode }) {
  const pc = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0
  return <div className={'prgs c-' + c}>
    {(label || sub) && <div className="prgs-h"><span>{label}</span><small>{sub}</small></div>}
    <div className="prgs-t" role="progressbar" aria-valuemin={0} aria-valuemax={max} aria-valuenow={value}><i style={{ width: pc + '%' }} /></div>
  </div>
}

export interface Slice { label: string; value: number; c: string; to?: string }
/** Donut chart (SVG). Colours come from the colour families (c-green …) so they follow the theme. */
export function Donut({ data, size = 168, thick = 20, center, legend = true }: { data: Slice[]; size?: number; thick?: number; center?: ReactNode; legend?: boolean }) {
  const total = data.reduce((a, b) => a + Math.max(0, b.value), 0)
  const r = (size - thick) / 2, C = 2 * Math.PI * r
  const gap = data.filter(d => d.value > 0).length > 1 ? Math.min(6, C * 0.012) : 0
  let off = 0
  return <div className="donut">
    <div className="donut-c" style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={data.map(d => d.label + ' ' + d.value).join(', ')}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" className="donut-bg" strokeWidth={thick} />
        {total > 0 && data.map(d => {
          if (d.value <= 0) return null
          const len = (d.value / total) * C
          const seg = <circle key={d.label} className={'donut-s c-' + d.c} cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={thick} strokeLinecap="butt"
            strokeDasharray={`${Math.max(0.5, len - gap)} ${C}`} strokeDashoffset={-off} transform={`rotate(-90 ${size / 2} ${size / 2})`}><title>{d.label}: {d.value}</title></circle>
          off += len
          return seg
        })}
      </svg>
      {center !== undefined && <div className="donut-m">{center}</div>}
    </div>
    {legend && <div className="legend">{data.map(d => {
      const inner = <><i /><span>{d.label}</span><b>{d.value}</b></>
      return d.to ? <Link key={d.label} to={d.to} className={'c-' + d.c}>{inner}</Link> : <div key={d.label} className={'c-' + d.c}>{inner}</div>
    })}</div>}
  </div>
}

export interface Point { label: string; value: number; hint?: string }
/** Smooth area chart (SVG) with hover read-out. */
export function AreaChart({ data, height = 220, fmt = (n: number) => String(n), c = 'indigo' }: { data: Point[]; height?: number; fmt?: (n: number) => string; c?: string }) {
  const id = useId().replace(/:/g, '')
  const [hov, setHov] = useState<number | null>(null)
  const W = 640, H = height, P = { l: 34, r: 12, t: 14, b: 26 }
  const max = Math.max(1, ...data.map(d => d.value))
  const nice = useMemo(() => { const p = Math.pow(10, Math.floor(Math.log10(max))); const s = [1, 2, 2.5, 5, 10].find(x => x * p >= max / 4) || 10; return Math.ceil(max / (s * p)) * s * p }, [max])
  const x = (i: number) => P.l + (data.length <= 1 ? (W - P.l - P.r) / 2 : (i * (W - P.l - P.r)) / (data.length - 1))
  const y = (v: number) => P.t + (H - P.t - P.b) * (1 - v / nice)
  const pts = data.map((d, i) => [x(i), y(d.value)] as const)
  // Catmull-Rom → cubic Bézier for a smooth line
  let line = ''
  pts.forEach((p, i) => {
    if (!i) { line = `M${p[0]},${p[1]}`; return }
    const p0 = pts[i - 2] || pts[i - 1], p1 = pts[i - 1], p2 = p, p3 = pts[i + 1] || p
    const t = 0.18
    const c1 = [p1[0] + (p2[0] - p0[0]) * t, Math.min(y(0), p1[1] + (p2[1] - p0[1]) * t)]
    const c2 = [p2[0] - (p3[0] - p1[0]) * t, Math.min(y(0), p2[1] - (p3[1] - p1[1]) * t)]
    line += ` C${c1[0]},${c1[1]} ${c2[0]},${c2[1]} ${p2[0]},${p2[1]}`
  })
  const area = pts.length ? `${line} L${pts[pts.length - 1][0]},${y(0)} L${pts[0][0]},${y(0)} Z` : ''
  const ticks = [0, 0.25, 0.5, 0.75, 1].map(f => nice * f)
  const every = Math.max(1, Math.ceil(data.length / 8))
  const move = (e: PointerEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    const px = ((e.clientX - r.left) / r.width) * W
    let best = 0, bd = Infinity
    pts.forEach((p, i) => { const d = Math.abs(p[0] - px); if (d < bd) { bd = d; best = i } })
    setHov(best)
  }
  const h = hov !== null ? data[hov] : null
  return <div className={'area c-' + c}>
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" style={{ height }} onPointerMove={move} onPointerLeave={() => setHov(null)} role="img" aria-label={data.map(d => d.label + ': ' + fmt(d.value)).join(', ')}>
      <defs><linearGradient id={'ag' + id} x1="0" y1="0" x2="0" y2="1"><stop offset="0" className="area-s0" /><stop offset="1" className="area-s1" /></linearGradient></defs>
      {ticks.map(t => <g key={t}><line className="area-g" x1={P.l} x2={W - P.r} y1={y(t)} y2={y(t)} /><text className="area-y" x={P.l - 8} y={y(t) + 3.5} textAnchor="end">{fmt(Math.round(t))}</text></g>)}
      {area && <path d={area} fill={`url(#ag${id})`} />}
      {line && <path d={line} className="area-l" fill="none" vectorEffect="non-scaling-stroke" />}
      {data.map((d, i) => i % every === 0 || i === data.length - 1 ? <text key={i} className="area-x" x={x(i)} y={H - 6} textAnchor={i === 0 ? 'start' : i === data.length - 1 ? 'end' : 'middle'}>{d.label}</text> : null)}
      {hov !== null && <g><line className="area-h" x1={pts[hov][0]} x2={pts[hov][0]} y1={P.t} y2={y(0)} vectorEffect="non-scaling-stroke" /></g>}
    </svg>
    {hov !== null && h && <>
      <span className="area-d" style={{ left: (pts[hov][0] / W) * 100 + '%', top: (pts[hov][1] / H) * 100 + '%' }} />
      <div className={'area-t' + (pts[hov][0] / W > 0.7 ? ' l' : '')} style={{ left: (pts[hov][0] / W) * 100 + '%', top: (pts[hov][1] / H) * 100 + '%' }}><b>{fmt(h.value)}</b><small>{h.hint || h.label}</small></div>
    </>}
  </div>
}

/** Loading placeholder block. */
export function Skeleton({ h = 14, w = '100%', r, className = '' }: { h?: number | string; w?: number | string; r?: number | string; className?: string }) {
  return <span className={'skel ' + className} style={{ height: h, width: w, borderRadius: r }} aria-hidden="true" />
}

/** Empty state with a small illustration. */
export function EmptyState({ title, children, action, ic = 'inbox' }: { title: string; children?: ReactNode; action?: ReactNode; ic?: string }) {
  return <div className="empty es">
    <svg className="es-i" viewBox="0 0 160 110" aria-hidden="true">
      <ellipse cx="80" cy="98" rx="58" ry="7" className="es-sh" />
      <rect x="30" y="22" width="100" height="68" rx="14" className="es-b" />
      <rect x="44" y="38" width="44" height="8" rx="4" className="es-l" /><rect x="44" y="54" width="72" height="6" rx="3" className="es-l2" /><rect x="44" y="66" width="56" height="6" rx="3" className="es-l2" />
      <circle cx="122" cy="26" r="16" className="es-a" /><path d="M115 26h14M122 19v14" className="es-p" />
    </svg>
    <span className="es-ic"><Icon n={ic} /></span>
    <h3>{title}</h3>{children && <p className="muted">{children}</p>}{action}
  </div>
}
