// Shared UI kit – v1 markup and class names (styles/app.css) restyled by the v2 design tokens,
// plus v2 components: Avatar, Trend, Progress, Donut, AreaChart, Skeleton, EmptyState.
import React, { ReactNode, useEffect, useState, MouseEvent, InputHTMLAttributes, useRef, useId, useMemo, PointerEvent, KeyboardEvent as KEvent } from 'react'
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

/** KPI card – a dark "island" with the colour family as accent. Optional sparkline and tone (pri = deep brand, ok = deep green). */
export function Kpi({ label, value, ic, c, foot, to, spark, tone }: { label: string; value: ReactNode; ic: string; c: string; foot?: ReactNode; to?: string; spark?: number[]; tone?: 'pri' | 'ok' }) {
  const inner = <><span className="st-i"><Icon n={ic} /></span><span className="st-l">{label}</span><b className="st-v">{value}</b>
    {spark && spark.length > 1 && <span className="st-sp"><Sparkline data={spark} w={84} h={30} /></span>}
    {foot !== undefined && foot !== '' && <small className="st-f">{foot}</small>}</>
  const cls = 'stat st-' + c + (tone ? ' tone-' + tone : '')
  return to ? <Link to={to} className={cls}>{inner}</Link> : <div className={cls}>{inner}</div>
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

/** Tag-style editor for a list of values (e.g. dropdown options). `value` stays a newline-separated string,
 *  exactly what the server stores; chips can be removed with ×, new ones are added with the button or Enter. */
export function ChipsInput({ value, onChange, placeholder = 'Add an option', addLabel = 'Add', disabled, label }: { value: string; onChange: (v: string) => void; placeholder?: string; addLabel?: string; disabled?: boolean; label?: string }) {
  const items = (value || '').split('\n').map(x => x.trim()).filter(Boolean)
  const [txt, setTxt] = useState('')
  const [dup, setDup] = useState(false)
  const add = () => {
    const parts = txt.split(/[\n,]/).map(x => x.trim()).filter(Boolean)
    if (!parts.length) return
    const next = [...items]
    let clash = false
    parts.forEach(x => { if (next.some(y => y.toLowerCase() === x.toLowerCase())) clash = true; else next.push(x) })
    setDup(clash && next.length === items.length)
    onChange(next.join('\n')); setTxt('')
  }
  const del = (i: number) => onChange(items.filter((_, j) => j !== i).join('\n'))
  const key = (e: KEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') { e.preventDefault(); add() }
    else if (e.key === 'Backspace' && !txt && items.length) del(items.length - 1)
  }
  return <div className={'chin' + (disabled ? ' off' : '')} role="group" aria-label={label || 'Options'}>
    {items.length > 0 ? <ul className="chin-l">{items.map((x, i) => <li key={x + i} className="chin-c"><span>{x}</span>
      {!disabled && <button type="button" onClick={() => del(i)} aria-label={'Remove ' + x} title="Remove"><Icon n="x" /></button>}</li>)}</ul>
      : <span className="chin-e">No options yet</span>}
    {!disabled && <div className="chin-a"><input value={txt} placeholder={placeholder} aria-label={placeholder} onChange={e => { setTxt(e.target.value); setDup(false) }} onKeyDown={key} />
      <button type="button" className="btn btn-sm" onClick={add} disabled={!txt.trim()}><Icon n="plus" /><span>{addLabel}</span></button></div>}
    {dup && <small className="chin-w">That option is already in the list.</small>}
  </div>
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
  const [hov, setHov] = useState<string | null>(null)
  const hs = hov ? data.find(d => d.label === hov) : null
  let off = 0
  return <div className={'donut' + (hov ? ' hov' : '')}>
    <div className="donut-c" style={{ width: size, height: size }} onPointerLeave={() => setHov(null)}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={data.map(d => d.label + ' ' + d.value).join(', ')}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" className="donut-bg" strokeWidth={thick} />
        {total > 0 && data.map(d => {
          if (d.value <= 0) return null
          const len = (d.value / total) * C
          const seg = <circle key={d.label} className={'donut-s c-' + d.c + (hov === d.label ? ' on' : '')} cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={thick} strokeLinecap="butt"
            style={{ ['--len' as string]: Math.max(0.5, len - gap), ['--C' as string]: C } as React.CSSProperties}
            strokeDasharray={`${Math.max(0.5, len - gap)} ${C}`} strokeDashoffset={-off} transform={`rotate(-90 ${size / 2} ${size / 2})`}
            onPointerEnter={() => setHov(d.label)}><title>{d.label}: {d.value}</title></circle>
          off += len
          return seg
        })}
      </svg>
      {hs ? <div className={'donut-m c-' + hs.c}><b>{hs.value.toLocaleString('en-IN')}</b><small>{hs.label}{total > 0 ? ' · ' + Math.round((hs.value / total) * 100) + '%' : ''}</small></div>
        : center !== undefined && <div className="donut-m">{center}</div>}
    </div>
    {legend && <div className="legend">{data.map(d => {
      const inner = <><i /><span>{d.label}</span><b>{d.value.toLocaleString('en-IN')}</b></>
      const ev = { onPointerEnter: () => setHov(d.label), onPointerLeave: () => setHov(null), onFocus: () => setHov(d.label), onBlur: () => setHov(null) }
      const cls = 'c-' + d.c + (hov === d.label ? ' on' : '')
      return d.to ? <Link key={d.label} to={d.to} className={cls} {...ev}>{inner}</Link> : <div key={d.label} className={cls} {...ev}>{inner}</div>
    })}</div>}
  </div>
}

export interface Point { label: string; value: number; hint?: string }
/** Smooth area chart (SVG) with hover read-out. */
export function AreaChart({ data, height = 220, fmt = (n: number) => String(n), c = 'indigo' }: { data: Point[]; height?: number; fmt?: (n: number) => string; c?: string }) {
  const id = useId().replace(/:/g, '')
  const [hov, setHov] = useState<number | null>(null)
  const box = useRef<HTMLDivElement>(null)
  const [W, setW] = useState(640)
  useEffect(() => {
    const el = box.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(e => { const w = Math.round(e[0].contentRect.width); if (w > 0) setW(w) })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  const H = height, P = { l: 34, r: 12, t: 14, b: 26 }
  const max = Math.max(4, ...data.map(d => d.value))
  const nice = useMemo(() => { const raw = max / 4, p = Math.pow(10, Math.floor(Math.log10(raw))); const step = ([1, 2, 5, 10].find(x => x * p >= raw) || 10) * p; return Math.max(4, step * 4) }, [max])
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
  const every = Math.max(1, Math.ceil(data.length / Math.max(2, Math.floor(W / 80))))
  const move = (e: PointerEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    const px = ((e.clientX - r.left) / r.width) * W
    let best = 0, bd = Infinity
    pts.forEach((p, i) => { const d = Math.abs(p[0] - px); if (d < bd) { bd = d; best = i } })
    setHov(best)
  }
  const h = hov !== null ? data[hov] : null
  return <div className={'area c-' + c} ref={box}>
    <svg viewBox={`0 0 ${W} ${H}`} width={W} height={H} onPointerMove={move} onPointerLeave={() => setHov(null)} role="img" aria-label={data.map(d => d.label + ': ' + fmt(d.value)).join(', ')}>
      <defs><linearGradient id={'ag' + id} x1="0" y1="0" x2="0" y2="1"><stop offset="0" className="area-s0" /><stop offset="1" className="area-s1" /></linearGradient></defs>
      {ticks.map(t => <g key={t}><line className="area-g" x1={P.l} x2={W - P.r} y1={y(t)} y2={y(t)} /><text className="area-y" x={P.l - 8} y={y(t) + 3.5} textAnchor="end">{fmt(Math.round(t))}</text></g>)}
      {area && <path d={area} fill={`url(#ag${id})`} />}
      {line && <path d={line} className="area-l" fill="none" pathLength={1} vectorEffect="non-scaling-stroke" />}
      {data.map((d, i) => (i % every === 0 && data.length - 1 - i >= every * 0.6) || i === data.length - 1 ? <text key={i} className="area-x" x={x(i)} y={H - 6} textAnchor={i === 0 ? 'start' : i === data.length - 1 ? 'end' : 'middle'}>{d.label}</text> : null)}
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

/** Tiny trend line for KPI cards (no axes). Colour = current --c. */
export function Sparkline({ data, w = 96, h = 32, fill = true }: { data: number[]; w?: number; h?: number; fill?: boolean }) {
  const id = useId().replace(/:/g, '')
  if (data.length < 2) return null
  const mx = Math.max(...data), mn = Math.min(...data), rg = mx - mn || 1
  const pts = data.map((v, i) => [(i / (data.length - 1)) * (w - 4) + 2, h - 3 - ((v - mn) / rg) * (h - 8)] as const)
  const line = pts.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ',' + p[1].toFixed(1)).join(' ')
  const last = pts[pts.length - 1]
  return <svg className="spk" width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-hidden="true">
    <defs><linearGradient id={'sp' + id} x1="0" y1="0" x2="0" y2="1"><stop offset="0" className="spk-s0" /><stop offset="1" className="spk-s1" /></linearGradient></defs>
    {fill && <path d={line + ` L${last[0]},${h} L${pts[0][0]},${h} Z`} fill={`url(#sp${id})`} />}
    <path d={line} className="spk-l" fill="none" pathLength={1} />
    <circle cx={last[0]} cy={last[1]} r={2.8} className="spk-d" />
  </svg>
}

export interface Bar { label: string; value: number; hint?: string }
/** Vertical bar chart with animated bars and a hover / focus read-out. */
export function BarChart({ data, height = 200, fmt = (n: number) => n.toLocaleString('en-IN'), c = 'indigo', every }: { data: Bar[]; height?: number; fmt?: (n: number) => string; c?: string; every?: number }) {
  const [hov, setHov] = useState<number | null>(null)
  const max = Math.max(1, ...data.map(d => d.value))
  const nice = (() => { const raw = max / 4, p = Math.pow(10, Math.floor(Math.log10(raw || 1))); const st = ([1, 2, 2.5, 5, 10].find(x => x * p >= raw) || 10) * p; return st * 4 })()
  const step = every || Math.max(1, Math.ceil(data.length / 8))
  const h = hov !== null ? data[hov] : null
  return <div className={'bch c-' + c} style={{ ['--h' as string]: height + 'px' } as React.CSSProperties} onPointerLeave={() => setHov(null)}>
    <div className="bch-y" aria-hidden="true">{[1, .75, .5, .25, 0].map(f => <span key={f}>{fmt(Math.round(nice * f * 100) / 100)}</span>)}</div>
    <div className="bch-p">
      <div className="bch-g" aria-hidden="true">{[0, 1, 2, 3, 4].map(i => <i key={i} />)}</div>
      <div className="bch-b" role="list" aria-label="Chart">{data.map((d, i) => <button type="button" key={i} role="listitem" className={'bch-c' + (hov === i ? ' on' : '')}
        aria-label={(d.hint || d.label) + ': ' + fmt(d.value)} onPointerEnter={() => setHov(i)} onFocus={() => setHov(i)} onBlur={() => setHov(null)}>
        <i style={{ height: (d.value / nice) * 100 + '%', animationDelay: Math.min(i * 18, 500) + 'ms' }} /></button>)}</div>
      {h && hov !== null && <div className={'bch-t' + (hov / data.length > 0.66 ? ' l' : '')} style={{ left: ((hov + 0.5) / data.length) * 100 + '%', bottom: Math.min(92, (h.value / nice) * 100) + '%' }}><b>{fmt(h.value)}</b><small>{h.hint || h.label}</small></div>}
      <div className="bch-x" aria-hidden="true">{data.map((d, i) => <span key={i}>{i % step === 0 || i === data.length - 1 ? d.label : ''}</span>)}</div>
    </div>
  </div>
}

/** Empty state with an illustration (pass a cartoon from components/Cartoons as `art`). */
export function EmptyState({ title, children, action, ic = 'inbox', art }: { title: string; children?: ReactNode; action?: ReactNode; ic?: string; art?: ReactNode }) {
  if (art) return <div className="empty es es-art"><div className="es-art-i">{art}</div><h3>{title}</h3>{children && <p className="muted">{children}</p>}{action}</div>
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
