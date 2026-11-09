// All activity across leads (v1 activity.php).
import { Link, useSearchParams } from 'react-router-dom'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Icon, Pager, Spinner, usePageTitle } from '@/components/ui'
import { EVIC } from './common'

const TYPES: [string, string][] = [['found', 'New leads'], ['stage', 'Status changes'], ['sent', 'Messages sent'], ['replied', 'Replies'], ['booked', 'Bookings'], ['call', 'Calls'], ['note', 'Notes'], ['error', 'Errors']]

interface Res { rows: { id: number; lead_id: number; type: string; channel: string; message: string; created_at: string; name: string; contact_name: string }[]; total: number; page: number; pages: number; capped: boolean }

export default function Activity() {
  usePageTitle('All activity', 'Everything that happened across your leads')
  const [sp, setSp] = useSearchParams()
  const type = sp.get('type') || '', p = sp.get('p') || ''
  const q = useQuery({ queryKey: ['events', type, p], queryFn: () => api.get<Res>('/events', { type, p }), placeholderData: keepPreviousData })
  const d = q.data
  const go = (x: Record<string, string>) => setSp(Object.fromEntries(Object.entries({ type, ...x }).filter(([, v]) => v)))
  return <>
    <Link className="back" to="/dashboard">← Dashboard</Link>
    <div className="tabs mt"><a href="#" className={type === '' ? 'on' : ''} onClick={e => { e.preventDefault(); go({ type: '', p: '' }) }}>All{type === '' && d && <em>{d.total.toLocaleString('en-IN')}{d.capped ? '+' : ''}</em>}</a>
      {TYPES.map(([k, lb]) => <a key={k} href="#" className={type === k ? 'on' : ''} onClick={e => { e.preventDefault(); go({ type: k, p: '' }) }}>{lb}</a>)}</div>
    <div className="card">{!d ? <Spinner /> : !d.rows.length ? <div className="empty sm"><Icon n="activity" /><p>No activity here yet.</p></div> :
      <ul className="tl" style={{ marginTop: 0 }}>{d.rows.map(x => <li key={x.id} className={'ev-' + x.type}><i><Icon n={EVIC[x.type] || 'zap'} /></i><div><span><Link to={'/leads/' + x.lead_id}><b>{x.contact_name || x.name}</b></Link> · {x.message}</span>
        <small>{new Date(x.created_at).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', hour12: true })}{x.channel ? ' · ' + x.channel : ''}</small></div></li>)}</ul>}
      {d && d.pages > 1 && <div style={{ margin: '14px -20px -20px' }}><Pager page={d.page} pages={d.pages} total={d.total} label={d.capped ? 'events (showing the latest)' : 'events'} onPage={n => go({ p: String(n) })} /></div>}
    </div>
  </>
}
