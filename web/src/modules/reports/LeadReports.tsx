// "Audit & report" card for the lead page: latest reports, view / PDF / copy link and (re-)run the
// AI report with a chosen prompt. Exported for the crm lead page.
import { FormEvent, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ago } from '@/lib/format'
import { useCan, useMod } from '@/lib/session'
import { Btn, Card, Field, Icon, Modal, toast } from '@/components/ui'
import { Prompt, ReportItem, copyText } from './common'

export default function LeadReports({ leadId }: { leadId: number }) {
  const can = useCan(), mod = useMod()
  const qc = useQueryClient()
  const r = useQuery({ queryKey: ['lead-reports', leadId], queryFn: () => api.get<{ reports: ReportItem[]; mode: string }>('/leads/' + leadId + '/reports'), enabled: mod('prospecting') })
  const [open, setOpen] = useState(false)
  if (!mod('prospecting') || !r.data) return null
  const rep = r.data.reports[0]
  return <Card title="Audit & report" ic="file" c="orange" actions={<>
    {rep && <><span className="muted sm">Report {ago(rep.created_at)} · viewed {rep.views}×</span>
      <a className="btn btn-sm" href={rep.url} target="_blank" rel="noreferrer"><Icon n="eye" />View</a>
      <a className="btn btn-sm" href={'/api/reports/' + rep.id + '/pdf'}><Icon n="download" />PDF</a>
      <Btn sm ic="link" title="Copy public link" onClick={() => copyText(rep.url)} /></>}
    {can('prospecting') && <Btn sm v="p" ic="zap" onClick={() => setOpen(true)}>{rep ? 'Re-run report' : 'Write report'}</Btn>}
  </>}>
    {r.data.reports.length > 1 && <ul className="ul">{r.data.reports.slice(1, 6).map(x => <li key={x.id}><a className="lnk" href={x.url} target="_blank" rel="noreferrer">Report of {new Date(x.created_at).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })}</a> <span className="muted sm">· {x.prompt_name || '—'} · viewed {x.views}×</span></li>)}</ul>}
    {!r.data.reports.length && <p className="muted sm mt0">No report yet.</p>}
    <RunReport leadId={leadId} open={open} onClose={() => setOpen(false)} onDone={() => qc.invalidateQueries({ queryKey: ['lead-reports', leadId] })} />
  </Card>
}

/** Modal: write / re-write the AI report of a lead with a chosen prompt. */
export function RunReport({ leadId, open, onClose, onDone }: { leadId: number; open: boolean; onClose: () => void; onDone?: () => void }) {
  const p = useQuery({ queryKey: ['prompts', 'report'], queryFn: () => api.get<Prompt[]>('/prompts'), enabled: open })
  const [f, setF] = useState({ pw: '', pn: '', contact: false })
  const [busy, setBusy] = useState(false)
  const pw = (p.data || []).filter(x => x.type === 'report_website'), pn = (p.data || []).filter(x => x.type === 'report_noweb')
  const sel = (k: 'pw' | 'pn', list: Prompt[]) => <select value={f[k]} onChange={e => setF({ ...f, [k]: e.target.value })}><option value="">Prompt in use</option>{list.map(x => <option key={x.id} value={x.id}>{x.name}{x.is_active ? ' (in use)' : ''}</option>)}</select>
  const run = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try { const r = await api.post('/leads/' + leadId + '/report', { pw: Number(f.pw) || 0, pn: Number(f.pn) || 0, contact: f.contact }); toast(r.message); onClose(); onDone?.() } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  return <Modal open={open} onClose={onClose} title="AI report" ic="zap" c="violet">
    <form onSubmit={run}>
      <p className="muted sm">Researches the market and writes a branded report from the latest audit. Watch the progress on the lead.</p>
      <div className="fg">
        <Field label="Report prompt (has website)">{sel('pw', pw)}</Field>
        <Field label="Report prompt (no website)">{sel('pn', pn)}</Field>
        <label className="chk"><input type="checkbox" checked={f.contact} onChange={e => setF({ ...f, contact: e.target.checked })} /> Send the report to the business when it is ready</label>
      </div>
      <div className="md-f"><Btn onClick={onClose}>Cancel</Btn><Btn v="p" type="submit" ic="zap" busy={busy}>Start</Btn></div>
    </form>
  </Modal>
}
