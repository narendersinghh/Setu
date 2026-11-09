// Runtime theming: brand (white label) from GET /api/public/brand + light / dark / system mode.
// Brand colours, fonts and radius are written as CSS variables on <html>; styles/tokens.css derives every
// other shade from them. The mode is stored per browser in localStorage ("setu-theme").
import { createContext, ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { Icon } from '@/components/ui'

export type ThemePref = 'light' | 'dark' | 'system'
export type ThemeMode = 'light' | 'dark'

export interface Brand {
  name: string
  tagline: string
  logo: string        // for light backgrounds
  logo_dark: string   // for dark backgrounds
  favicon: string
  font_heading: string
  font_body: string
  colors: { primary: string; accent: string; success: string; warning: string; danger: string; info: string }
  radius: number
  theme_default: ThemePref
  signup: boolean
}

/** Defaults from docs/V2_PLATFORM.md (used until / unless the server provides a brand). */
export const DEFAULT_BRAND: Brand = {
  name: 'Setu',
  tagline: 'Bridge every lead to conversion',
  logo: '',
  logo_dark: '',
  favicon: '',
  font_heading: 'Plus Jakarta Sans',
  font_body: 'Inter',
  colors: { primary: '#C21F3A', accent: '#FFD60A', success: '#6BA539', warning: '#F59E0B', danger: '#E5484D', info: '#3B82F6' },
  radius: 16,
  theme_default: 'light',
  signup: true,
}

/** Current brand name – used for document titles outside React (usePageTitle). */
export let brandName = DEFAULT_BRAND.name

const KEY = 'setu-theme'
const store = {
  get(): ThemePref | null { try { const v = localStorage.getItem(KEY); return v === 'light' || v === 'dark' || v === 'system' ? v : null } catch { return null } },
  set(v: ThemePref) { try { localStorage.setItem(KEY, v) } catch { /* private mode */ } },
}

const isColor = (s: unknown): s is string => typeof s === 'string' && /^(#[0-9a-f]{3,8}|rgba?\([\d\s.,%]+\)|hsla?\([\d\s.,%deg]+\))$/i.test(s.trim())
const isFont = (s: unknown): s is string => typeof s === 'string' && /^[\w \-]{2,60}$/.test(s.trim())
const isUrl = (s: unknown): s is string => typeof s === 'string' && (/^https?:\/\//i.test(s) || /^\/(?!\/)/.test(s) || /^data:image\//i.test(s))

function merge(raw: any): Brand {
  const b: Brand = { ...DEFAULT_BRAND, colors: { ...DEFAULT_BRAND.colors } }
  if (!raw || typeof raw !== 'object') return b
  if (typeof raw.name === 'string' && raw.name.trim()) b.name = raw.name.trim()
  if (typeof raw.tagline === 'string') b.tagline = raw.tagline
  for (const k of ['logo', 'logo_dark', 'favicon'] as const) if (isUrl(raw[k])) b[k] = raw[k]
  if (isFont(raw.font_heading)) b.font_heading = raw.font_heading.trim()
  if (isFont(raw.font_body)) b.font_body = raw.font_body.trim()
  if (raw.colors && typeof raw.colors === 'object') for (const k of Object.keys(b.colors) as (keyof Brand['colors'])[]) if (isColor(raw.colors[k])) b.colors[k] = raw.colors[k].trim()
  const r = Number(raw.radius)
  if (raw.radius !== undefined && raw.radius !== null && raw.radius !== '' && Number.isFinite(r)) b.radius = Math.min(28, Math.max(0, r))
  if (raw.theme_default === 'light' || raw.theme_default === 'dark' || raw.theme_default === 'system') b.theme_default = raw.theme_default
  if (typeof raw.signup === 'boolean') b.signup = raw.signup
  return b
}

/** Relative luminance of a colour (any CSS colour the browser understands). */
function luminance(c: string) {
  let r = 0, g = 0, b = 0
  const h = c.trim()
  if (h[0] === '#') {
    let x = h.slice(1)
    if (x.length === 3 || x.length === 4) x = x.slice(0, 3).split('').map(ch => ch + ch).join('')
    r = parseInt(x.slice(0, 2), 16); g = parseInt(x.slice(2, 4), 16); b = parseInt(x.slice(4, 6), 16)
  } else {
    const m = h.match(/[\d.]+/g)
    if (!m || h.startsWith('hsl')) return 0.2
    ;[r, g, b] = m.slice(0, 3).map(Number)
  }
  const f = (v: number) => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4) }
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}
/** Black or white text, whichever has more contrast on the colour. */
export const inkOn = (c: string) => (luminance(c) > 0.42 ? '#111111' : '#FFFFFF')

function fontsHref(b: Brand) {
  const fam = (f: string, w: string) => 'family=' + encodeURIComponent(f).replace(/%20/g, '+') + ':wght@' + w
  const fs = [fam(b.font_heading, '500;600;700;800')]
  if (b.font_body !== b.font_heading) fs.push(fam(b.font_body, '400;500;600;700'))
  return 'https://fonts.googleapis.com/css2?' + fs.join('&') + '&display=swap'
}

function applyBrand(b: Brand) {
  const s = document.documentElement.style
  const c = b.colors
  s.setProperty('--pri', c.primary); s.setProperty('--pri-ink', inkOn(c.primary))
  s.setProperty('--acc', c.accent); s.setProperty('--acc-ink', inkOn(c.accent))
  s.setProperty('--ok', c.success); s.setProperty('--warn', c.warning); s.setProperty('--err', c.danger); s.setProperty('--info', c.info)
  s.setProperty('--r', b.radius + 'px')
  s.setProperty('--fh', `'${b.font_heading}', '${b.font_body}', system-ui, sans-serif`)
  s.setProperty('--ff', `'${b.font_body}', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif`)
  // Google Fonts (display=swap); the default pair is already linked in index.html
  let link = document.getElementById('brand-fonts') as HTMLLinkElement | null
  const href = fontsHref(b)
  if (!link) { link = document.createElement('link'); link.id = 'brand-fonts'; link.rel = 'stylesheet'; document.head.appendChild(link) }
  if (link.href !== href) link.href = href
  if (b.favicon) {
    let ic = document.querySelector('link[rel="icon"]') as HTMLLinkElement | null
    if (!ic) { ic = document.createElement('link'); ic.rel = 'icon'; document.head.appendChild(ic) }
    ic.href = b.favicon
  }
  const meta = document.querySelector('meta[name="theme-color"]') as HTMLMetaElement | null
  if (meta) meta.content = c.primary
  if (brandName !== b.name) {
    document.title = document.title.replace(new RegExp(' · ' + brandName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '$'), '') + ' · ' + b.name
    brandName = b.name
  }
}

interface ThemeCtx { brand: Brand; pref: ThemePref; mode: ThemeMode; setPref: (p: ThemePref) => void; loaded: boolean }
const Ctx = createContext<ThemeCtx>({ brand: DEFAULT_BRAND, pref: 'light', mode: 'light', setPref: () => undefined, loaded: false })

const sysDark = () => typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [brand, setBrand] = useState<Brand>(DEFAULT_BRAND)
  const [loaded, setLoaded] = useState(false)
  const [stored, setStored] = useState<ThemePref | null>(store.get)
  const [dark, setDark] = useState(sysDark)
  const pref: ThemePref = stored || brand.theme_default
  const mode: ThemeMode = pref === 'system' ? (dark ? 'dark' : 'light') : pref

  useEffect(() => {
    let off = false
    fetch('/api/public/brand', { credentials: 'same-origin' })
      .then(r => (r.ok ? r.json() : null))
      .catch(() => null)
      .then(raw => { if (!off) { setBrand(merge(raw)); setLoaded(true) } })
    return () => { off = true }
  }, [])
  useEffect(() => { applyBrand(brand) }, [brand])
  useEffect(() => {
    if (typeof matchMedia !== 'function') return
    const mq = matchMedia('(prefers-color-scheme: dark)')
    const f = () => setDark(mq.matches)
    mq.addEventListener?.('change', f)
    return () => mq.removeEventListener?.('change', f)
  }, [])
  useEffect(() => {
    const el = document.documentElement
    el.setAttribute('data-theme', mode)
    el.style.colorScheme = mode
  }, [mode])

  const setPref = useCallback((p: ThemePref) => { store.set(p); setStored(p) }, [])
  const v = useMemo(() => ({ brand, pref, mode, setPref, loaded }), [brand, pref, mode, setPref, loaded])
  return <Ctx.Provider value={v}>{children}</Ctx.Provider>
}

export const useTheme = () => useContext(Ctx)
export const useBrand = () => useContext(Ctx).brand

const NEXT: Record<ThemePref, ThemePref> = { light: 'dark', dark: 'system', system: 'light' }
const LABEL: Record<ThemePref, string> = { light: 'Light theme', dark: 'Dark theme', system: 'System theme' }
const ICON: Record<ThemePref, string> = { light: 'sun', dark: 'moon', system: 'monitor' }

/** Icon button cycling light → dark → system. */
export function ThemeToggle({ className = 'ib bd' }: { className?: string }) {
  const { pref, setPref } = useTheme()
  const next = NEXT[pref]
  return <button type="button" className={className + ' theme-t'} onClick={() => setPref(next)} title={LABEL[pref] + ' – switch to ' + LABEL[next].toLowerCase()} aria-label={LABEL[pref] + '. Switch to ' + LABEL[next].toLowerCase()}>
    <Icon n={ICON[pref]} />
  </button>
}

/** Brand mark: uploaded logo for the given background, else a monogram in brand colours. */
export function BrandMark({ on = 'light', size = 40 }: { on?: 'light' | 'dark'; size?: number }) {
  const b = useBrand()
  const src = on === 'dark' ? b.logo_dark || b.logo : b.logo || b.logo_dark
  if (src) return <img className="logo-i" src={src} alt={b.name} style={{ width: size, height: size }} />
  return <span className="logo-m" style={{ width: size, height: size, fontSize: Math.round(size * 0.48) }} aria-hidden="true">{(b.name.trim()[0] || 'S').toUpperCase()}<i /></span>
}
