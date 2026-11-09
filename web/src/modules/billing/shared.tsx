// Billing data (GET /api/billing, see docs/V2_PLATFORM.md "Billing & payment gateways") with a graceful fallback
// to /api/me + public plans while the endpoint is missing, plus the plan-limit usage grid shared with the usage page.
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe, planName, daysLeft, PlanStatus } from '@/lib/session'
import { Badge, Icon } from '@/components/ui'
import { date } from '@/lib/format'

export interface BPlan {
  id: number; name: string; slug?: string; description?: string; price_month: number; price_year: number; currency: string
  trial_days?: number; highlight?: boolean; features?: string[] | null; limits?: Record<string, number>; modules?: string[]; is_default?: boolean
}
export interface BLimit { key: string; label: string; limit: number; used: number }
export interface Payment { id: number; plan: string; cycle: string; amount: number; currency: string; status: string; gateway: string; created_at: string; paid_at: string | null }
export interface Billing {
  plan: { id: number; name: string; price_month: number; price_year: number; currency: string } | null
  status: PlanStatus | string
  trial_ends_at: string | null
  expires_at: string | null
  effective_plan?: { id: number; name: string } | string | null
  limits: BLimit[]
  plans: BPlan[]
  gateways: { key: string; name: string }[]
  payments: Payment[]
}

/** Label and icon for each plan limit key (docs/V2_PLATFORM.md "Plans & limits"). */
export const LIMIT_META: Record<string, [string, string]> = {
  users: ['Team members', 'users'], leads: ['Leads & scraped businesses', 'target'], automations: ['Automations', 'bot'],
  sources: ['Lead capture sources', 'plug'], searches_month: ['Map searches / month', 'map'], reports_month: ['AI reports / month', 'file'],
  emails_month: ['Emails / month', 'mail'], calls_month: ['AI calls / month', 'phone'],
}
export const LIMIT_ORDER = ['users', 'leads', 'emails_month', 'automations', 'sources', 'searches_month', 'reports_month', 'calls_month']
const ord = (k: string) => { const i = LIMIT_ORDER.indexOf(k); return i < 0 ? 99 : i }

const SYM: Record<string, string> = { INR: '₹', USD: '$', EUR: '€', GBP: '£', AUD: 'A$', CAD: 'C$', SGD: 'S$', AED: 'AED ' }
export const sym = (cur?: string) => (cur ? SYM[cur] ?? cur + ' ' : '₹')
export const price = (v: number, cur?: string) => (v ? sym(cur) + Number(v).toLocaleString('en-IN', { maximumFractionDigits: 2 }) : 'Free')

/** Work out the plan status when the server does not send one. */
function derive(trial?: string | null, expires?: string | null, paid?: boolean): string {
  const tl = daysLeft(trial), el = daysLeft(expires)
  if (tl !== null && tl >= 0) return 'trial'
  if (el !== null && el >= 0) return 'active'
  if (!paid) return 'free'
  return tl !== null || el !== null ? 'expired' : 'active'
}

/** Billing state: the real endpoint when it exists, else a read-only view built from /me and the public plans. */
export function useBilling() {
  const me = useMe()
  const q = useQuery({ queryKey: ['billing'], queryFn: () => api.get<Billing>('/billing'), retry: false, staleTime: 30_000 })
  const pub = useQuery({ queryKey: ['public-plans'], queryFn: () => api.get<BPlan[]>('/public/plans').catch(() => [] as BPlan[]), enabled: q.isError, staleTime: 300_000 })
  if (q.data && typeof q.data === 'object' && !q.isError) {
    const d = q.data
    const limits = (Array.isArray(d.limits) ? d.limits : []).slice().sort((a, b) => ord(a.key) - ord(b.key))
    return { data: { ...d, limits, plans: d.plans || [], gateways: d.gateways || [], payments: d.payments || [], status: d.status || derive(d.trial_ends_at, d.expires_at, !!d.plan?.price_month) } as Billing, live: true, loading: false, error: '' }
  }
  if (q.isLoading || (q.isError && pub.isLoading)) return { data: null, live: false, loading: true, error: '' }
  const plans = Array.isArray(pub.data) ? pub.data : []
  const pn = planName(me.org)
  const pid = me.org.plan && typeof me.org.plan === 'object' ? me.org.plan.id : 0
  const cur = plans.find(p => p.id === pid) || plans.find(p => p.name === pn)
  const limits: BLimit[] = Object.entries(me.limits || {}).filter(([, v]) => v).map(([k, v]) => ({ key: k, label: LIMIT_META[k]?.[0] || k, limit: v.limit, used: v.used }))
  limits.sort((a, b) => ord(a.key) - ord(b.key))
  const d: Billing = {
    plan: pn ? { id: cur?.id || pid, name: pn, price_month: cur?.price_month || 0, price_year: cur?.price_year || 0, currency: cur?.currency || 'INR' } : null,
    status: me.org.plan_status || derive(me.org.trial_ends_at, me.org.plan_expires_at, !!cur?.price_month),
    trial_ends_at: me.org.trial_ends_at || null, expires_at: me.org.plan_expires_at || null,
    limits, plans, gateways: [], payments: [],
  }
  return { data: d, live: false, loading: false, error: (q.error as Error | null)?.message || '' }
}

export function StatusBadge({ status, trial }: { status: string; trial?: string | null }) {
  const tl = daysLeft(trial)
  if (status === 'trial') return <Badge c="gold" dot>{tl !== null && tl >= 0 ? 'Trial · ' + tl + ' day' + (tl === 1 ? '' : 's') + ' left' : 'Trial'}</Badge>
  if (status === 'active') return <Badge c="green" dot>Active</Badge>
  if (status === 'expired') return <Badge c="red" dot>Expired</Badge>
  if (status === 'free') return <Badge c="blue" dot>Free</Badge>
  return <Badge>{status || '—'}</Badge>
}

/** One sentence that explains the plan status. */
export function statusText(b: Billing) {
  const tl = daysLeft(b.trial_ends_at), el = daysLeft(b.expires_at)
  switch (b.status) {
    case 'trial': return tl === null ? 'You are on a free trial.' : tl <= 0 ? 'Your free trial ends today.' : `Your free trial ends on ${date(b.trial_ends_at)} – ${tl} day${tl === 1 ? '' : 's'} left.`
    case 'active': return b.expires_at ? `Paid until ${date(b.expires_at)}${el !== null && el <= 14 ? ' – renews soon' : ''}.` : 'Your plan is active.'
    case 'expired': return 'Your paid period has ended, so the free plan limits apply until you renew.'
    default: return 'You are on the free plan. Upgrade any time for higher limits.'
  }
}

/** Plan limits as usage bars with the remaining amount. */
export function LimitGrid({ limits }: { limits: BLimit[] }) {
  if (!limits.length) return <p className="muted sm mb0">No limits – your plan is unlimited.</p>
  return <div className="lim">{limits.map(l => {
    const [lb, ic] = LIMIT_META[l.key] || [l.label || l.key, 'zap']
    const inf = !l.limit
    const pc = inf ? 0 : Math.min(100, Math.round((l.used / l.limit) * 100))
    const left = inf ? null : Math.max(0, l.limit - l.used)
    return <div key={l.key} className={'lim-i c-' + (pc >= 90 ? 'rose' : pc >= 70 ? 'amber' : 'green')}>
      <div className="lim-h"><Icon n={ic} /><span className="ell">{lb || l.label}</span></div>
      <div className="lim-v"><b>{l.used.toLocaleString('en-IN')}</b><span>/ {inf ? 'Unlimited' : l.limit.toLocaleString('en-IN')}</span></div>
      <div className={'lim-t' + (inf ? ' inf' : '')} role="progressbar" aria-label={lb} aria-valuemin={0} aria-valuemax={l.limit || 100} aria-valuenow={l.used}><i className={pc >= 90 ? 'hot' : pc >= 70 ? 'warn' : ''} style={inf ? undefined : { width: Math.max(pc, l.used ? 2 : 0) + '%' }} /></div>
      <div className="lim-f"><span>{inf ? 'No limit on your plan' : pc + '% used'}</span>{left !== null && <span><b>{left.toLocaleString('en-IN')}</b> left</span>}</div>
    </div>
  })}</div>
}
