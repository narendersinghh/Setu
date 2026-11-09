// Shared bits of the CRM pages: metadata (statuses, sources, owners, custom fields), badges, helpers.
import { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Icon } from '@/components/ui'

export interface Opt { key: string; label: string; color: string; icon?: string }
export interface CField { id: number; fkey: string; label: string; type: string; options: string; required: boolean; sort: number }
export interface Meta {
  statuses: Opt[]; sources: Opt[]; stages: Record<string, { label: string; color: string }>
  users: { id: number; name: string; role: string }[] | null; fields: CField[]; services: string[] | null
  cc: string; currency_symbol: string; admin: boolean; me: number
}

export function useMeta() {
  return useQuery({ queryKey: ['crm-meta'], queryFn: () => api.get<Meta>('/meta/crm'), staleTime: 60_000 })
}

export interface LeadRow {
  id: number; name: string; contact_name: string; city: string; service: string; tags: string; phone: string; email: string
  source: string; source_detail: string; status: string; deal_value: number | null; follow_up_at: string | null
  assigned_to: number | null; owner: string | null; last_activity: string | null; created_at: string
}

// v1 status colours → chip colour classes
export const CC: Record<string, string> = { blue: 'blue', teal: 'teal', purple: 'violet', orange: 'orange', gold: 'amber', green: 'green', red: 'rose', gray: 'gray' }

export const STATUSES: Opt[] = [
  { key: 'new', label: 'New', color: 'blue' }, { key: 'contacted', label: 'Contacted', color: 'teal' }, { key: 'qualified', label: 'Qualified', color: 'purple' },
  { key: 'meeting', label: 'Meeting booked', color: 'orange' }, { key: 'proposal', label: 'Proposal sent', color: 'gold' }, { key: 'won', label: 'Won', color: 'green' }, { key: 'lost', label: 'Lost', color: 'red' },
]
export const SOURCES: Record<string, [string, string, string]> = { maps: ['Map scraper', 'orange', 'map'], manual: ['Manual', 'gray', 'edit'], website: ['Website form', 'blue', 'globe'], facebook: ['Meta / Facebook ads', 'purple', 'target'], google: ['Google Ads', 'green', 'zap'], webhook: ['Webhook / API', 'teal', 'plug'], import: ['CSV import', 'gray', 'upload'], booking: ['Booking page', 'orange', 'cal'] }

export function statusOf(k: string) { return STATUSES.find(s => s.key === k) || { key: k, label: k ? k[0].toUpperCase() + k.slice(1) : '—', color: 'gray' } }

export function StatusBadge({ s }: { s: string }) { const x = statusOf(s); return <span className={'bdg bdg-' + x.color}>{x.label}</span> }

export function SrcBadge({ s }: { s: string }) {
  const [lb, c, ic] = SOURCES[s] || [s ? s[0].toUpperCase() + s.slice(1) : '—', 'gray', 'link']
  return <span className={'src-b src-' + c}><Icon n={ic} />{lb}</span>
}

const STAGES: Record<string, [string, string]> = { new: ['New', 'gray'], queued: ['Queued', 'blue'], auditing: ['Auditing', 'blue'], reporting: ['Writing report', 'purple'], report_ready: ['Report ready', 'gold'], contacting: ['Sending', 'blue'], contacted: ['Contacted', 'teal'], opened: ['Opened', 'teal'], replied: ['Replied', 'green'], won: ['Won', 'green'], lost: ['Not interested', 'red'], failed: ['Failed', 'red'], skipped: ['Skipped', 'gray'] }
export function StageBadge({ s }: { s: string }) { const a = STAGES[s] || [s, 'gray']; return <span className={'bdg bdg-' + a[1]}>{a[0]}</span> }
export const stageLabel = (s: string) => (STAGES[s] || [s])[0]

export function ChBadge({ s }: { s: string }) {
  const m: Record<string, string> = { sent: 'teal', delivered: 'teal', read: 'green', opened: 'green', failed: 'red', manual: 'gold', queued: 'blue' }
  return s ? <span className={'bdg bdg-' + (m[s] || 'gray')}>{s[0].toUpperCase() + s.slice(1)}</span> : <span className="muted">—</span>
}

const AV = ['indigo', 'violet', 'blue', 'sky', 'teal', 'green', 'amber', 'orange', 'rose', 'pink']
// crc32-like stable colour per name
export function avColor(s: string) { let h = 0; for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0; return AV[h % 10] }

export const tagList = (t: string) => (t || '').split(',').map(x => x.trim()).filter(Boolean)
export const telHref = (p: string) => 'tel:' + (p || '').replace(/[^\d+]/g, '')

/** v1 phone_norm: digits with country code. */
export function phoneNorm(p: string, cc = '91') {
  p = (p || '').trim()
  if (!p) return ''
  const plus = p[0] === '+'
  let d = p.replace(/\D+/g, '')
  if (!d) return ''
  if (plus) return d
  if (d.startsWith('00')) return d.slice(2)
  d = d.replace(/^0+/, '')
  return d.length <= 10 ? cc.replace(/\D+/g, '') + d : d
}
export const waLink = (p: string, cc: string, text = '') => 'https://wa.me/' + phoneNorm(p, cc) + (text ? '?text=' + encodeURIComponent(text) : '')

export const isLate = (fu: string | null, status: string) => !!fu && new Date(fu).getTime() <= Date.now() && status !== 'won' && status !== 'lost'

export function fmtShort(ts: string) {
  const d = new Date(ts)
  return d.toLocaleDateString('en-GB', { day: '2-digit', month: 'short' }) + ', ' + d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
}

export const EVIC: Record<string, string> = { found: 'pin', enriched: 'globe', audited: 'activity', stage: 'zap', sent: 'send', opened: 'eye', viewed: 'eye', replied: 'msg', error: 'alert', note: 'edit', scheduled: 'clock', wa_manual: 'wa', delivered: 'check', read: 'check', call: 'mic', auto: 'bot', booked: 'cal' }

/** Form control for a custom field (v1 CRM::input). */
export function CustomInput({ f, value, onChange }: { f: CField; value: string; onChange: (v: string) => void }) {
  const req = f.required
  switch (f.type) {
    case 'textarea': return <textarea rows={2} required={req} value={value} onChange={e => onChange(e.target.value)} />
    case 'select': return <select required={req} value={value} onChange={e => onChange(e.target.value)}><option value="">—</option>{f.options.split('\n').map(x => x.trim()).filter(Boolean).map(x => <option key={x}>{x}</option>)}</select>
    case 'checkbox': return <span className="chk"><input type="checkbox" checked={!!value} onChange={e => onChange(e.target.checked ? 'Yes' : '')} /> Yes</span>
    default: {
      const t = ({ number: 'number', date: 'date', email: 'email', phone: 'tel', url: 'url' } as Record<string, string>)[f.type] || 'text'
      return <input type={t} required={req} value={value} onChange={e => onChange(e.target.value)} />
    }
  }
}

/** Datetime-local value → ISO string with the browser's offset (server accepts RFC 3339). */
export function localToISO(v: string) { if (!v) return ''; const d = new Date(v); return isNaN(d.getTime()) ? v : d.toISOString() }

export function Muted({ children = '—' }: { children?: ReactNode }) { return <span className="muted">{children}</span> }

export function CopyText(t: string) { try { navigator.clipboard?.writeText(t) } catch { /* ignore */ } }
