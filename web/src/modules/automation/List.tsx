import { Link, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Alert, Btn, Hi, Icon, IconBtn, Kpi, Spinner, Switch, act, confirmDo, toast, usePageTitle } from '@/components/ui'
import { AutoRow, Flow, Meta } from './shared'

interface ListRes { rows: AutoRow[]; stats: { total: number; on: number; running: number; completed: number; week: number }; ready: Meta['ready'] }

export default function Automations() {
  usePageTitle('Automations', 'Follow up automatically – WhatsApp, email, AI calls, bookings and status updates')
  const qc = useQueryClient(), nav = useNavigate()
  const q = useQuery({ queryKey: ['automations'], queryFn: () => api.get<ListRes>('/automations') })
  const meta = useQuery({ queryKey: ['meta', 'automation'], queryFn: () => api.get<Meta>('/meta/automation'), staleTime: 60_000 })
  if (!q.data || !meta.data) return <Spinner />
  const { rows, stats, ready } = q.data
  const T = Object.fromEntries(meta.data.triggers.map(t => [t.key, t.label]))
  const refresh = () => qc.invalidateQueries({ queryKey: ['automations'] })
  const toggle = async (r: AutoRow, on: boolean) => { await act(() => api.post('/automations/' + r.id + '/toggle', { on }), 'Saved'); refresh() }
  const remove = async (r: AutoRow) => { if (!confirmDo('Delete this automation?')) return; await act(() => api.del('/automations/' + r.id), 'Automation deleted.'); refresh() }
  const copy = async (r: AutoRow) => { const res = await act(() => api.post<{ id: number; message: string }>('/automations/' + r.id + '/duplicate')); if (res) { toast(res.message); nav('/automations/' + res.id) } }
  const missing = [
    !ready.email && <Link key="e" className="lnk" to="/settings/email">Email</Link>,
    !ready.whatsapp && <span key="w"><Link className="lnk" to="/settings/whatsapp">WhatsApp API</Link> (manual mode only prepares messages)</span>,
    !ready.voice && <Link key="v" className="lnk" to="/settings/voice">AI voice calls</Link>,
  ].filter(Boolean)
  return <>
    <div className="stats">
      <Kpi label="Automations on" value={stats.on} ic="bot" c="violet" foot={stats.total + ' in total'} />
      <Kpi label="Running now" value={stats.running} ic="activity" c="blue" foot="leads inside a flow" />
      <Kpi label="Completed" value={stats.completed} ic="check" c="green" foot="finished runs" />
      <Kpi label="Started this week" value={stats.week} ic="zap" c="orange" foot="new runs in 7 days" />
    </div>
    {missing.length > 0 && <Alert>For fully automatic follow-ups connect {missing.map((m, i) => <span key={i}>{i > 0 && ' · '}{m}</span>)}.</Alert>}
    <div className="ph">
      <div><b style={{ fontSize: 15, color: 'var(--ink)' }}>Your automations</b> <span className="muted sm">· switch on, edit or remove</span></div>
      <div className="pha"><Link className="btn" to="/automations/recipes"><Icon n="spark" />Browse ready-made recipes</Link><Link className="btn btn-p" to="/automations/new"><Icon n="plus" />New automation</Link></div>
    </div>
    {rows.length === 0 ? <div className="card"><div className="empty"><Hi n="bot" c="gray" lg /><h4>No automations yet</h4><p>Start from a ready-made recipe (welcome message, follow-up sequence, AI call…) or build your own flow.</p>
      <div className="row" style={{ justifyContent: 'center' }}><Link className="btn btn-p" to="/automations/recipes"><Icon n="spark" />Browse recipes</Link><Link className="btn" to="/automations/new"><Icon n="plus" />Build from scratch</Link></div></div></div>
      : <div className="aul">{rows.map(r => <div key={r.id} className={'card au-row' + (r.active ? '' : ' off')}>
        <Switch checked={r.active} title="On / off" onChange={v => toggle(r, v)} />
        <div className="grow">
          <Link to={'/automations/' + r.id}><b>{r.name}</b></Link>
          <div className="au-when"><Icon n="zap" />When {(T[r.trig] || r.trig).toLowerCase()}{r.trig_value !== '' && <> · <b>{r.trig_value}</b></>}</div>
          <div className="mt"><Flow steps={r.steps} actions={meta.data!.actions} /></div>
        </div>
        <div className="au-n"><b>{r.running}</b><span>running</span></div>
        <div className="au-n"><b>{r.completed}</b><span>completed</span></div>
        <div className="row nw">
          <Link className="btn btn-sm" to={'/automations/' + r.id}><Icon n="edit" />Edit</Link>
          <IconBtn ic="copy" title="Duplicate" onClick={() => copy(r)} />
          <IconBtn ic="trash" title="Delete" onClick={() => remove(r)} />
        </div>
      </div>)}</div>}
  </>
}
