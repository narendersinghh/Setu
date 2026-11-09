// Activity & processes drawer (owned by the ai-engine module) – same markup as the v1 drawer in layout.php
// (drw-ov, drw, drw-h, drw-b) and the v1 A.refresh() list: map searches, audits & reports with progress,
// recent errors, scheduled outreach and running automations. Refreshes every 3 seconds while open.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Btn, Icon, IconBtn, Spinner, confirmDo, toast } from '@/components/ui'
import { ProcessModal, abortLeads } from '@/modules/ai/Process'

interface Activity {
  active: { id: number; name: string; stage: string; progress: number; progress_msg: string; last_error: string; source: string }[]
  failed: { id: number; name: string; last_error: string; last_activity: string }[]
  search: { id: number; keyword: string; location: string; found: number; limit_n: number }[]
  scheduled: { id: number; name: string; run_after: string }[]
  runs: { id: number; status: string; next_at: string | null; name: string; lead: string }[]
  bulk?: { queued: number; running: number; sent: number; mode: string; slots: number }
  queue?: { due: number; later: number; running: number; next: number | null }
  now: number
}

export default function ActivityDrawer({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const [on, setOn] = useState(false)
  const [proc, setProc] = useState(0)
  const q = useQuery({ queryKey: ['activity'], queryFn: () => api.get<Activity>('/activity'), refetchInterval: 3000 })
  // slide in / out like v1 (the .drw transition runs when "on" toggles)
  useEffect(() => { const t = requestAnimationFrame(() => setOn(true)); return () => cancelAnimationFrame(t) }, [])
  const close = () => { setOn(false); setTimeout(onClose, 250) }
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape' && !proc) close() }
    addEventListener('keydown', k)
    return () => removeEventListener('keydown', k)
  }, [proc])
  const refresh = () => qc.invalidateQueries({ queryKey: ['activity'] })
  const post = async (url: string, body: object, ok: string) => {
    try { await api.post(url, body); toast(ok) } catch (e: any) { toast(e.message) }
    refresh()
  }
  const stopAll = async () => {
    if (!confirmDo('Stop ALL running searches, audits, reports and scheduled outreach?')) return
    await post('/activity/stop-all', {}, 'Everything stopped')
    qc.invalidateQueries({ queryKey: ['process'] })
  }
  const abort = async (id: number) => { if (await abortLeads([id])) { refresh(); qc.invalidateQueries({ queryKey: ['leads'] }) } }
  const s = q.data
  const when = (t: string) => new Date(t).toLocaleString([], { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })
  return <>
    <div className={'drw-ov' + (on ? ' on' : '')} onClick={close} />
    <aside className={'drw' + (on ? ' on' : '')} aria-label="Activity">
      <div className="drw-h"><h3>Activity</h3><div className="row"><Btn sm v="r" ic="stop" onClick={stopAll}>Stop all</Btn><IconBtn ic="x" title="Close" onClick={close} /></div></div>
      <div className="drw-b">
        {!s ? <Spinner /> : <>
          {!!s.search.length && <><div className="dsec">Map searches</div>{s.search.map(c =>
            <div className="ji" key={c.id}><div className="ji-h"><b>{c.keyword}{c.location ? ' · ' + c.location : ''}</b><Link className="lnk" to={'/campaigns/' + c.id} onClick={close}>Open</Link></div>
              <div className="pb"><i style={{ width: Math.round(c.found / Math.max(1, c.limit_n) * 100) + '%' }} /></div><small className="pbm">{c.found} of {c.limit_n} listings found</small></div>)}</>}
          <div className="dsec">Audits & reports ({s.active.length})</div>
          {!s.active.length && <p className="muted sm">Nothing is processing right now.</p>}
          {s.active.map(l => <div className="ji" key={l.id}><div className="ji-h"><Link className="ji-n" to={'/leads/' + l.id} onClick={close}><b>{l.name}</b></Link></div>
            <div className="pb"><i style={{ width: (+l.progress) + '%' }} /></div>
            <div className="ji-f"><small className="pbm">{+l.progress}% · {l.progress_msg || l.stage}</small>
              <span className="ji-a"><Btn xs onClick={() => setProc(l.id)}>View process</Btn><Btn xs v="r" onClick={() => abort(l.id)}>Abort</Btn></span></div></div>)}
          {!!s.bulk && (s.bulk.queued + s.bulk.running + s.bulk.sent) > 0 && <p className="muted sm"><Icon n="spark" /> AI engine: {s.bulk.mode === 'batch'
            ? `${s.bulk.sent} in a Claude batch, ${s.bulk.queued} waiting to be sent`
            : `${s.bulk.running} being written (${s.bulk.slots} at the same time), ${s.bulk.queued} waiting`}</p>}
          {!!s.failed.length && <><div className="dsec">Recent errors</div>{s.failed.map(l =>
            <div className="ji err" key={l.id}><div className="ji-h"><Link className="ji-n" to={'/leads/' + l.id} onClick={close}><b>{l.name}</b></Link><Btn xs onClick={() => setProc(l.id)}>View process</Btn></div>
              <span className="err-t">{l.last_error || 'Failed'}</span></div>)}</>}
          {!!s.scheduled.length && <><div className="dsec">Scheduled outreach ({s.scheduled.length})</div>{s.scheduled.map(j =>
            <div className="ji" key={j.id}><div className="ji-h"><Link to={'/leads/' + j.id} onClick={close}><b>{j.name}</b></Link><Btn xs onClick={() => post('/activity/unschedule', { id: j.id }, 'Outreach cancelled')}>Cancel</Btn></div>
              <small className="pbm">Sends at {when(j.run_after)}</small></div>)}</>}
          {!!s.runs.length && <><div className="dsec">Automations running ({s.runs.length})</div>{s.runs.map(r =>
            <div className="ji" key={r.id}><div className="ji-h"><b>{r.name}</b><Btn xs onClick={() => post('/activity/cancel-run', { id: r.id }, 'Automation run cancelled')}>Cancel</Btn></div>
              <small className="pbm">{r.lead} · {r.status === 'waiting' && r.next_at ? 'next step ' + when(r.next_at) : 'running'}</small></div>)}</>}
        </>}
      </div>
    </aside>
    {proc > 0 && <ProcessModal leadId={proc} onClose={() => setProc(0)} />}
  </>
}
