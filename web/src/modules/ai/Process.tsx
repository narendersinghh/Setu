// Process log ("View process") of a lead's background work and the live progress box of the lead page.
// Same markup as v1 (#m-proc / A.procLoad and #lp). Other modules import { ProcessModal, LeadProgress } from '@/modules/ai/Process'.
import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Badge, Btn, Modal, Spinner, confirmDo, toast } from '@/components/ui'

export interface Step {
  id: number; job_id: number; kind: 'start' | 'step' | 'ai'; step: string; status: 'run' | 'ok' | 'err' | 'warn'; detail: string
  model: string; tokens_in: number; tokens_out: number; cost: number; ms: number; at: number
}
export interface Process {
  id: number; name: string; stage: string; stage_label: string; busy: boolean; progress: number; msg: string; error: string; can_abort: boolean
  steps?: Step[]; totals?: { in: number; out: number; cost: number; ai_ms: number; ai: number }; now?: number
  jobs?: { id: number; type: string; status: string; title: string; run_after: string }[]
}

export const kn = (n: number) => { n = +n || 0; return n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1) + 'k' : String(n) }
export const usd = (c: number) => { c = +c || 0; return c ? '$' + (c < 0.01 ? c.toFixed(4) : c.toFixed(3)) : '–' }
export const dur = (ms: number) => { const s = Math.round((ms || 0) / 1000); return s < 60 ? s + 's' : Math.floor(s / 60) + 'm ' + (s % 60) + 's' }

/** Stop the work of these leads (asks first). */
export async function abortLeads(ids: number[]) {
  if (!confirmDo('Stop this process?')) return false
  try {
    const r = await api.post<{ n: number }>('/activity/abort', { ids })
    toast((r.n || 0) + ' process(es) stopped')
    return true
  } catch (e: any) { toast(e.message); return false }
}

export function useProcess(leadId: number, lite = false, enabled = true) {
  return useQuery({
    queryKey: ['process', leadId, lite],
    queryFn: () => api.get<Process>('/leads/' + leadId + '/process', lite ? { lite: 1 } : undefined),
    enabled: enabled && leadId > 0,
    refetchInterval: q => (q.state.data?.busy ? (lite ? 2500 : 2000) : (lite ? false : 8000)),
    retry: false,
  })
}

/** The process log body (live while the lead is busy). */
export function ProcessLog({ leadId }: { leadId: number }) {
  const q = useProcess(leadId)
  const qc = useQueryClient()
  const wrap = useRef<HTMLDivElement>(null)
  const atEnd = useRef(true)
  useEffect(() => { const w = wrap.current; if (w && atEnd.current) w.scrollTop = w.scrollHeight }, [q.data])
  if (q.isError) return <p className="muted">You do not have access to this process.</p>
  const s = q.data
  if (!s) return <Spinner />
  const t = s.totals || { in: 0, out: 0, cost: 0, ai_ms: 0, ai: 0 }
  const steps = s.steps || []
  const abort = async () => { if (await abortLeads([s.id])) qc.invalidateQueries({ queryKey: ['process', leadId] }) }
  return <>
    <div className="pr-h"><div className="grow"><Link className="lnk" to={'/leads/' + s.id}><b>{s.name}</b></Link> <Badge c={s.busy ? 'blue' : s.stage === 'failed' ? 'red' : 'green'}>{s.stage_label}</Badge></div>
      {s.busy && s.can_abort && <Btn xs v="r" onClick={abort}>Abort</Btn>}</div>
    {s.busy ? <><div className="pb"><i style={{ width: s.progress + '%' }} /></div><small className="pbm pr-m"><span className="spin" />{s.progress}% · {s.msg}</small></>
      : s.error ? <div className="al al-err mb0">{s.error}</div> : null}
    <div className="pr-k"><div><span>AI calls</span><b>{t.ai}</b></div><div><span>Tokens in</span><b>{kn(t.in)}</b></div><div><span>Tokens out</span><b>{kn(t.out)}</b></div><div><span>AI cost</span><b>{usd(t.cost)}</b></div><div><span>AI time</span><b>{dur(t.ai_ms)}</b></div></div>
    {!!s.jobs?.length && <p className="muted sm">{s.jobs.map(j => (j.status === 'running' ? 'Running: ' : 'Queued: ') + j.title).join(' · ')}</p>}
    {!steps.length ? <p className="muted sm">No steps recorded yet. Steps appear here as soon as the audit or report starts.</p> :
      <div className="pr-w" ref={wrap} onScroll={e => { const w = e.currentTarget; atEnd.current = w.scrollTop + w.clientHeight >= w.scrollHeight - 30 }}>
        <table className="tbl pr-t"><thead><tr><th>Time</th><th>Step</th><th className="num">Took</th><th>Model</th><th className="num">Tokens in / out</th><th className="num">Cost</th></tr></thead><tbody>
          {steps.map(r => {
            const tm = new Date(r.at * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
            if (r.kind === 'start') return <tr key={r.id} className="pr-g"><td>{tm}</td><td colSpan={5}>{r.step}</td></tr>
            const ic = r.status === 'run' ? <span className="spin" /> : r.status === 'err' ? <i className="pr-x">✕</i> : <i className="pr-ok">✓</i>
            return <tr key={r.id} className={'pr-' + r.status + (r.kind === 'ai' ? ' pr-ai' : '')}>
              <td className="nowrap muted">{tm}</td>
              <td><div className="pr-s">{ic}<div><b>{r.step}</b>{r.detail && <small>{r.detail}</small>}</div></div></td>
              <td className="num nowrap">{dur(r.ms)}</td><td className="sm">{r.model}</td>
              <td className="num nowrap">{r.kind === 'ai' ? kn(r.tokens_in) + ' / ' + kn(r.tokens_out) : ''}</td>
              <td className="num">{r.kind === 'ai' ? usd(r.cost) : ''}</td></tr>
          })}
        </tbody></table></div>}
  </>
}

/** "Process log" popup (v1 #m-proc). */
export function ProcessModal({ leadId, onClose }: { leadId: number; onClose: () => void }) {
  return <Modal open={leadId > 0} onClose={onClose} title="Process log" ic="activity" c="violet" size="xl"><ProcessLog leadId={leadId} /></Modal>
}

/** Live progress box on the lead page while an audit / report / send is running (v1 #lp). */
export function LeadProgress({ leadId, onDone }: { leadId: number; onDone?: () => void }) {
  const q = useProcess(leadId, true)
  const [open, setOpen] = useState(false)
  const was = useRef(false)
  const s = q.data
  useEffect(() => {
    if (!s) return
    if (was.current && !s.busy) onDone?.()
    was.current = s.busy
  }, [s?.busy])
  if (!s || !s.busy) return null
  return <>
    <div className="al al-warn lp"><span className="spin" /><div className="grow"><b className="lp-t">{s.stage_label} · {s.progress}%</b>
      <div className="pb" style={{ margin: '6px 0 2px' }}><i style={{ width: s.progress + '%' }} /></div><small className="lp-m">{s.msg}</small></div>
      <div className="lp-a"><Btn sm ic="activity" onClick={() => setOpen(true)}>View process</Btn>
        {s.can_abort && <Btn sm v="r" ic="stop" onClick={async () => { if (await abortLeads([s.id])) q.refetch() }}>Abort</Btn>}</div></div>
    {open && <ProcessModal leadId={leadId} onClose={() => setOpen(false)} />}
  </>
}
