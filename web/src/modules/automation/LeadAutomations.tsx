// Automations card for the lead page (crm can embed it: <LeadAutomations leadId={id} />).
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe } from '@/lib/session'
import { ago } from '@/lib/format'
import { Badge, Btn, Field, Hi, Icon, Modal, act } from '@/components/ui'
import { RUN_BADGE, Run, RunLog } from './shared'

interface Res { rows: Run[]; automations: { id: number; name: string; active: boolean }[] }

export function useLeadAutomations(leadId: number) {
  return useQuery({ queryKey: ['lead', leadId, 'automations'], queryFn: () => api.get<Res>('/leads/' + leadId + '/automations') })
}

export default function LeadAutomations({ leadId }: { leadId: number }) {
  const qc = useQueryClient(), me = useMe()
  const q = useLeadAutomations(leadId)
  const [open, setOpen] = useState(false)
  const [aid, setAid] = useState(0)
  if (!q.data) return null
  const autos = q.data.automations.filter(a => a.active)
  const refresh = () => { qc.invalidateQueries({ queryKey: ['lead', leadId] }) }
  const stop = async (r: Run) => { await act(() => api.post('/automation-runs/' + r.id + '/cancel'), 'Automation stopped for this lead.'); refresh() }
  const start = async () => {
    const r = await act(() => api.post('/leads/' + leadId + '/automations', { automation_id: aid || autos[0]?.id }), 'Automation started.')
    if (r) { setOpen(false); refresh() }
  }
  return <div className="card">
    <div className="card-h"><h3><Hi n="bot" c="pink" />Automations</h3>{autos.length > 0 && <Btn sm v="p" ic="play" onClick={() => setOpen(true)}>Start one</Btn>}</div>
    {q.data.rows.length === 0 && <p className="muted sm">No automation has run for this lead yet.</p>}
    {q.data.rows.map(r => <div key={r.id} className="msg">
      <div className="msg-h"><Icon n="bot" /><b>{r.automation}</b><Badge c={RUN_BADGE[r.status] || 'gray'}>{r.status}</Badge><small>{ago(r.created_at)}</small>
        {(r.status === 'running' || r.status === 'waiting') && <Btn xs onClick={() => stop(r)}>Stop</Btn>}</div>
      <RunLog log={r.log} tz={me.settings?.timezone} />
    </div>)}
    <Modal open={open} onClose={() => setOpen(false)} title="Start automation" ic="bot" c="pink" footer={<Btn v="p" ic="play" onClick={start}>Start</Btn>}>
      <Field label="Automation"><select value={aid || autos[0]?.id || 0} onChange={e => setAid(Number(e.target.value))}>{autos.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</select></Field>
    </Modal>
  </div>
}
