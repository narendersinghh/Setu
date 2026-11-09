// Plan & billing: current plan and status, limits as usage bars, pricing cards with monthly / yearly switch and
// gateway choice (POST /api/billing/checkout → hosted payment page), payment history and ?paid=1 / ?failed=1 banners.
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useBrand } from '@/lib/theme'
import { daysLeft } from '@/lib/session'
import { date, dateTime } from '@/lib/format'
import { Alert, Badge, Btn, Card, EmptyState, Hi, Icon, Skeleton, toast, usePageTitle } from '@/components/ui'
import { PiggyBank, Trophy } from '@/components/Cartoons'
import { BPlan, Billing, LimitGrid, StatusBadge, price, statusText, sym, useBilling } from './shared'

const GW_LABEL: Record<string, string> = { razorpay: 'Razorpay', stripe: 'Stripe' }
const PAY_C: Record<string, string> = { paid: 'green', captured: 'green', succeeded: 'green', pending: 'gold', created: 'gold', failed: 'red', cancelled: 'gray', canceled: 'gray', refunded: 'blue' }

export default function BillingPage({ embedded }: { embedded?: boolean }) {
  return embedded ? <BillingView /> : <Titled />
}

function Titled() {
  usePageTitle('Plan & billing', 'Your plan, limits, upgrades and payments')
  return <BillingView />
}

function BillingView() {
  const b = useBilling()
  const brand = useBrand()
  const qc = useQueryClient()
  const [p, setP] = useSearchParams()
  const paid = p.get('paid') === '1', failed = p.get('failed') === '1'
  useEffect(() => { if (paid) { qc.invalidateQueries({ queryKey: ['me'] }); qc.invalidateQueries({ queryKey: ['billing'] }) } }, [paid])
  const clear = () => { p.delete('paid'); p.delete('failed'); setP(p, { replace: true }) }
  if (b.loading || !b.data) return <><div className="card"><Skeleton h={120} /></div><div className="card"><Skeleton h={160} /></div></>
  const d = b.data
  return <>
    {paid && <div className="card dk c-green bil-ok" role="status">
      <div className="bil-h"><div><span className="eyebrow"><Icon n="check" />Payment received</span><h2>Thank you – you are all set!</h2>
        <p>Your payment went through and your plan is being activated. It can take a minute for the gateway to confirm; this page updates on its own.</p>
        <div className="bil-a"><Btn sm v="p" ic="check" onClick={clear}>Got it</Btn><Link className="btn btn-sm" to="/dashboard"><Icon n="grid" /><span>Go to the dashboard</span></Link></div></div>
        <Trophy w={150} /></div></div>}
    {failed && <Alert t="err"><b>The payment did not go through.</b> You have not been charged. Try again, or pick another payment method below. <a href="#" onClick={e => { e.preventDefault(); clear() }}>Dismiss</a></Alert>}
    {!b.live && <Alert t="info">Online payments are not available yet{b.error && /not found|404/i.test(b.error) ? '' : ''}. Your plan and limits are shown below – contact {brand.name} support to change plan.</Alert>}

    <Current d={d} />

    <Card title="Plan limits & usage" ic="activity" c="teal" actions={<Link className="lnk sm" to="/usage">API usage &amp; costs <Icon n="arrowR" /></Link>}>
      <LimitGrid limits={d.limits} />
    </Card>

    <Pricing d={d} live={b.live} />

    <Card p0 title="Payment history" ic="file" c="violet">
      {!d.payments.length ? <EmptyState ic="file" title="No payments yet">Receipts of plan purchases and renewals will be listed here.</EmptyState> :
        <div className="tw"><table className="tbl"><thead><tr><th>Date</th><th>Plan</th><th>Period</th><th className="num">Amount</th><th>Paid with</th><th>Status</th><th>Paid on</th></tr></thead>
          <tbody>{d.payments.map(x => <tr key={x.id}>
            <td className="nowrap">{date(x.created_at)}</td>
            <td><b>{x.plan}</b></td>
            <td>{x.cycle === 'year' ? 'Yearly' : x.cycle === 'month' ? 'Monthly' : x.cycle}</td>
            <td className="num nowrap"><b>{price(x.amount, x.currency)}</b></td>
            <td>{GW_LABEL[x.gateway] || x.gateway}</td>
            <td><Badge c={PAY_C[x.status] || 'gray'} dot>{x.status ? x.status[0].toUpperCase() + x.status.slice(1) : '—'}</Badge></td>
            <td className="nowrap">{x.paid_at ? dateTime(x.paid_at) : '—'}</td>
          </tr>)}</tbody></table></div>}
    </Card>
  </>
}

function Current({ d }: { d: Billing }) {
  const tl = daysLeft(d.trial_ends_at)
  const cur = d.plan
  const eff = typeof d.effective_plan === 'object' && d.effective_plan ? d.effective_plan.name : typeof d.effective_plan === 'string' ? d.effective_plan : ''
  const tone = d.status === 'expired' ? 'c-rose' : d.status === 'trial' ? 'c-amber' : 'c-green'
  return <div className={'card dk p0 ' + tone}>
    <div className="bil-h">
      <div>
        <span className="eyebrow"><Icon n="crown" />Current plan</span>
        <h2>{cur?.name || 'No plan'} <StatusBadge status={d.status} trial={d.trial_ends_at} /></h2>
        <p>{statusText(d)}</p>
        <div className="bil-m">
          {cur && cur.price_month > 0 && <span><Icon n="tag" />List price <b>{price(cur.price_month, cur.currency)}</b> / month{cur.price_year > 0 && <> · <b>{price(cur.price_year, cur.currency)}</b> / year</>}</span>}
          {d.status === 'trial' && tl !== null && tl >= 0 && <span><Icon n="clock" />Trial ends <b>{date(d.trial_ends_at)}</b></span>}
          {d.expires_at && <span><Icon n="cal" />{d.status === 'expired' ? 'Expired' : 'Paid until'} <b>{date(d.expires_at)}</b></span>}
          {d.status === 'expired' && eff && <span><Icon n="layers" />Running on <b>{eff}</b> limits</span>}
        </div>
        {(d.status === 'expired' || d.status === 'trial' || d.status === 'free') && <div className="bil-a"><a className="btn btn-p" href="#plans"><Icon n="rocket" /><span>{d.status === 'expired' ? 'Renew or upgrade' : 'Upgrade now'}</span></a></div>}
      </div>
      <PiggyBank w={170} />
    </div>
  </div>
}

function Pricing({ d, live }: { d: Billing; live: boolean }) {
  const brand = useBrand()
  const plans = d.plans
  const hasYear = plans.some(x => x.price_year > 0)
  const [yearly, setYearly] = useState(false)
  const [gw, setGw] = useState('')
  const [busy, setBusy] = useState(0)
  const gateway = gw || d.gateways[0]?.key || ''
  if (!plans.length) return null
  const save = (x: BPlan) => x.price_month > 0 && x.price_year > 0 ? Math.round((1 - x.price_year / (x.price_month * 12)) * 100) : 0
  const best = Math.max(0, ...plans.map(save))
  const buy = async (x: BPlan) => {
    setBusy(x.id)
    try {
      const r = await api.post<{ url: string }>('/billing/checkout', { plan_id: x.id, cycle: yearly && x.price_year > 0 ? 'year' : 'month', gateway })
      if (r?.url) { location.href = r.url; return }
      toast('The payment page could not be opened. Please try again.')
    } catch (e: any) { toast(e?.status === 404 ? 'Online payments are not available yet.' : e?.message || 'Could not start the payment.') }
    setBusy(0)
  }
  const canPay = live && d.gateways.length > 0
  return <section id="plans" aria-label="Plans">
    <div className="prc-h">
      <div className="card-h mb0" style={{ margin: 0 }}><h3><Hi n="rocket" c="indigo" />Choose your plan</h3></div>
      <div className="row">
        {d.gateways.length > 1 && <div className="gw" role="radiogroup" aria-label="Pay with">{d.gateways.map(g => <button key={g.key} type="button" role="radio" aria-checked={gateway === g.key} className={gateway === g.key ? 'on' : ''} onClick={() => setGw(g.key)}><i />{g.name || GW_LABEL[g.key] || g.key}</button>)}</div>}
        {hasYear && <div className="seg" role="group" aria-label="Billing period"><button type="button" className={!yearly ? 'on' : ''} onClick={() => setYearly(false)}>Monthly</button><button type="button" className={yearly ? 'on' : ''} onClick={() => setYearly(true)}>Yearly{best > 0 && <Badge c="green">−{best}%</Badge>}</button></div>}
      </div>
    </div>
    {live && !d.gateways.length && <Alert t="info">Online payment is not switched on yet. Contact {brand.name} support to upgrade your plan.</Alert>}
    <div className="prc">{plans.map(x => {
      const y = yearly && x.price_year > 0
      const v = y ? x.price_year : x.price_month
      const isCur = d.plan?.id === x.id
      const s = save(x)
      const label = isCur ? (d.status === 'expired' || d.status === 'trial' ? (d.status === 'trial' ? 'Buy this plan' : 'Renew') : 'Extend') : v > 0 ? 'Upgrade to ' + x.name : 'Free plan'
      return <div key={x.id} className={'prc-c' + (x.highlight ? ' dk' : '') + (isCur ? ' cur' : '')}>
        {x.highlight ? <span className="rb">Most popular</span> : isCur ? <span className="rb" style={{ background: 'var(--ok)', color: '#fff' }}>Your plan</span> : null}
        <div className="prc-n"><b>{x.name}</b>{isCur && x.highlight && <Badge c="green">Your plan</Badge>}</div>
        <div className="prc-p">{price(v, x.currency)}{v > 0 && <small>/ {y ? 'year' : 'month'}</small>}</div>
        <div className="prc-s">{y && s > 0 ? 'Save ' + s + '% – ' + sym(x.currency) + Math.round(x.price_month * 12 - x.price_year).toLocaleString('en-IN') + ' a year' : x.trial_days ? x.trial_days + '-day free trial' : ''}</div>
        {x.description && <p className="prc-d">{x.description}</p>}
        <ul className="prc-l">{(x.features || []).slice(0, 7).map(f => <li key={f}><Icon n="check" />{f}</li>)}
          {!x.features?.length && x.limits && Object.entries(x.limits).slice(0, 5).map(([k, n]) => <li key={k}><Icon n="check" />{n ? n.toLocaleString('en-IN') : 'Unlimited'} {k.replace('_month', ' / month').replace('_', ' ')}</li>)}</ul>
        {v > 0 ? <Btn v={x.highlight || !isCur ? 'p' : ''} ic={isCur ? 'refresh' : 'rocket'} busy={busy === x.id} disabled={!canPay || !!busy} title={canPay ? '' : 'Online payment is not available yet'} onClick={() => buy(x)}>{label}</Btn>
          : <Btn disabled ic={isCur ? 'check' : undefined}>{isCur ? 'Current plan' : label}</Btn>}
      </div>
    })}</div>
  </section>
}
