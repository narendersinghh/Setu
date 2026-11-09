import { FormEvent, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe } from '@/lib/session'
import { ago } from '@/lib/format'
import { Badge, Btn, Hi, Icon, IconBtn, Spinner, Switch, toast, usePageTitle } from '@/components/ui'
import { Action, Automation, CC, Cond, Meta, RUN_BADGE, Run, RunLog, Step } from './shared'

type KStep = Step & { _k: number }
type KCond = Cond & { _k: number }
let seq = 1
const key = () => seq++

const BLANK: Automation = { id: 0, name: '', trig: 'lead_created', trig_value: '', conditions: [], steps: [], stop_on_reply: true, active: false, runs: 0, created_at: '' }

/** Default values of a new step (same defaults as the v1 editor). */
function newStep(t: string): Step {
  switch (t) {
    case 'wait': return { t, n: 1, u: 'hours' }
    case 'followup': return { t, n: 1, u: 'days' }
    case 'booking': return { t, ch: 'whatsapp', subject: '', text: '' }
    case 'notify': return { t, to: '', subject: 'New lead – {{contact_name}}', text: '{{contact_name}} – {{phone}} – {{email}}' }
    case 'status': return { t, v: 'new' }
    default: return { t }
  }
}

export default function Editor() {
  const { id = 'new' } = useParams()
  const isNew = id === 'new'
  const nav = useNavigate(), qc = useQueryClient()
  const me = useMe()
  const meta = useQuery({ queryKey: ['meta', 'automation'], queryFn: () => api.get<Meta>('/meta/automation'), staleTime: 60_000 })
  const q = useQuery({ queryKey: ['automation', id], queryFn: () => api.get<Automation>('/automations/' + id), enabled: !isNew })
  const runs = useQuery({ queryKey: ['automation', id, 'runs'], queryFn: () => api.get<{ rows: Run[] }>('/automations/' + id + '/runs'), enabled: !isNew })
  const [au, setAu] = useState<Automation | null>(isNew ? BLANK : null)
  const [steps, setSteps] = useState<KStep[]>([])
  const [conds, setConds] = useState<KCond[]>([])
  const [busy, setBusy] = useState(false)
  const [dd, setDd] = useState(false)
  const ddRef = useRef<HTMLDivElement>(null)
  usePageTitle(au?.id ? au.name || 'Automation' : 'New automation', 'Trigger → conditions → steps')

  useEffect(() => {
    const src = isNew ? BLANK : q.data
    if (!src) return
    setAu({ ...src })
    setSteps((src.steps || []).map(s => ({ ...s, _k: key() })))
    setConds((src.conditions || []).map(c => ({ ...c, _k: key() })))
  }, [isNew, q.data])

  useEffect(() => {
    if (!dd) return
    const h = (e: MouseEvent) => { if (ddRef.current && !ddRef.current.contains(e.target as Node)) setDd(false) }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [dd])

  if (q.isError) return <div className="card"><div className="empty"><h4>Automation not found.</h4><Link className="lnk" to="/automations">← Automations</Link></div></div>
  if (!meta.data || !au) return <Spinner />
  const M = meta.data
  const AC: Record<string, Action> = Object.fromEntries(M.actions.map(a => [a.key, a]))
  const set = (p: Partial<Automation>) => setAu({ ...au, ...p })
  const setTrig = (trig: string) => set({ trig, trig_value: trig === 'no_reply' ? '3' : trig === 'status_changed' ? M.statuses[0]?.[0] || '' : '' })
  const upd = (i: number, p: Record<string, any>) => setSteps(steps.map((s, j) => j === i ? { ...s, ...p } : s))
  const move = (i: number, d: number) => { const j = i + d; if (j < 0 || j >= steps.length) return; const n = [...steps];[n[i], n[j]] = [n[j], n[i]]; setSteps(n) }
  const updC = (i: number, p: Partial<Cond>) => setConds(conds.map((c, j) => j === i ? { ...c, ...p } : c))

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body = { ...au, steps: steps.map(({ _k, ...s }) => s), conditions: conds.filter(c => c.f).map(({ _k, ...c }) => c) }
      const r = isNew ? await api.post('/automations', body) : await api.put('/automations/' + au.id, body)
      toast(r.message)
      if (r.warn) toast(r.warn)
      qc.invalidateQueries({ queryKey: ['automations'] })
      qc.invalidateQueries({ queryKey: ['automation', String(r.id)] })
      if (isNew) nav('/automations/' + r.id, { replace: true })
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }

  const trig = M.triggers.find(t => t.key === au.trig)
  const tz = me.settings?.timezone || undefined
  const sel = (s: Step, i: number, k: string, opts: [string, string][], style?: object) =>
    <select value={s[k] ?? ''} style={style} onChange={e => upd(i, { [k]: e.target.value })}>{opts.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
  const inp = (s: Step, i: number, k: string, ph: string, extra: object = {}) =>
    <input value={s[k] ?? ''} placeholder={ph} onChange={e => upd(i, { [k]: e.target.value })} {...extra} />
  const area = (s: Step, i: number, k: string, ph: string, rows: number) =>
    <textarea rows={rows} value={s[k] ?? ''} placeholder={ph} onChange={e => upd(i, { [k]: e.target.value })} />
  const chk = (s: Step, i: number, k: string, label: string) =>
    <label className="chk sm mt"><input type="checkbox" checked={!!Number(s[k] || 0) || s[k] === true} onChange={e => upd(i, { [k]: e.target.checked ? 1 : 0 })} /> {label}</label>

  const body = (s: Step, i: number) => {
    switch (s.t) {
      case 'wait': return <div className="row mt"><input type="number" min={1} className="sm-in" value={s.n ?? 1} onChange={e => upd(i, { n: Number(e.target.value) || 1 })} />{sel(s, i, 'u', [['minutes', 'minutes'], ['hours', 'hours'], ['days', 'days']], { maxWidth: 140 })}</div>
      case 'whatsapp': return <>{area(s, i, 'text', 'Message…', 4)}{chk(s, i, 'attach', 'Attach the audit PDF (if a report exists)')}</>
      case 'email': return <>{inp(s, i, 'subject', 'Subject')}{area(s, i, 'body', 'Email text…', 5)}{chk(s, i, 'attach', 'Attach the audit PDF (if a report exists)')}</>
      case 'call': return <>{inp(s, i, 'first', 'First sentence (blank = default from Settings)')}{area(s, i, 'task', 'What should the AI do on this call? (blank = default script)', 3)}</>
      case 'booking': return <><div className="row mt">{sel(s, i, 'ch', [['whatsapp', 'via WhatsApp'], ['email', 'via Email']], { maxWidth: 160 })}{(s.ch === 'email') && inp(s, i, 'subject', 'Email subject')}</div>{area(s, i, 'text', 'Message (the booking link is added automatically)', 3)}</>
      case 'status': return sel(s, i, 'v', M.statuses.map(x => [x[0], x[1]]))
      case 'tag': return inp(s, i, 'v', 'tag name')
      case 'assign': return sel(s, i, 'v', [['', 'Nobody'], ...M.users.map(u => [String(u.id), u.name] as [string, string])])
      case 'followup': return <div className="row mt"><span className="sm muted">Remind in</span><input type="number" min={1} className="sm-in" value={s.n ?? 1} onChange={e => upd(i, { n: Number(e.target.value) || 1 })} />{sel(s, i, 'u', [['days', 'days'], ['hours', 'hours']], { maxWidth: 120 })}</div>
      case 'notify': return <>{inp(s, i, 'to', 'Email to notify (blank = company email)')}{inp(s, i, 'subject', 'Subject')}{area(s, i, 'text', '', 2)}</>
      case 'report': return <>{chk(s, i, 'contact', 'Send the report by Email & WhatsApp when ready')}{!M.prospecting && <small className="muted">Needs the Prospecting module.</small>}</>
      case 'webhook': return <>{inp(s, i, 'url', 'https://hooks.zapier.com/…', { type: 'url' })}<small className="muted">Sends the lead as JSON (Zapier, Make, n8n, Google Sheets…)</small></>
    }
    return null
  }

  return <form onSubmit={save}>
    <div className="ph"><Link className="lnk" to="/automations">← Automations</Link>
      <div className="pha"><label className="chk"><Switch checked={au.active} onChange={v => set({ active: v })} /> Active</label><Btn v="y" type="submit" ic="check" busy={busy}>Save automation</Btn></div>
    </div>
    <div className="g3">
      <div className="col">
        <div className="card">
          <label>Name<input required value={au.name} placeholder="e.g. New Facebook lead – welcome + AI call" onChange={e => set({ name: e.target.value })} /></label>
          <div className="au-st trig" style={{ display: 'block' }}>
            <b className="row"><Hi n="zap" c="amber" />When this happens</b>
            <select className="mt" value={au.trig} onChange={e => setTrig(e.target.value)}>{M.triggers.map(t => <option key={t.key} value={t.key}>{t.label}</option>)}</select>
            {au.trig === 'lead_created' && <div className="mt"><label>Only for source<select value={au.trig_value} onChange={e => set({ trig_value: e.target.value })}><option value="">Any source</option>{M.sources.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select></label></div>}
            {au.trig === 'status_changed' && <div className="mt"><label>Status<select value={au.trig_value} onChange={e => set({ trig_value: e.target.value })}>{M.statuses.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select></label></div>}
            {au.trig === 'tag_added' && <div className="mt"><label>Tag<input value={au.trig_value} onChange={e => set({ trig_value: e.target.value })} /></label></div>}
            {au.trig === 'no_reply' && <div className="mt"><label>Days without reply<input type="number" min={1} value={au.trig_value} onChange={e => set({ trig_value: e.target.value })} /></label></div>}
            {trig && <p className="hint">{trig.desc}</p>}
          </div>
          <h4>Only if (optional)</h4>
          <div>{conds.map((c, i) => <div key={c._k} className="row cond mt">
            <select value={c.f} onChange={e => updC(i, { f: e.target.value })}>{M.conds.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select>
            <select value={c.op} onChange={e => updC(i, { op: e.target.value })}>{M.ops.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select>
            <input value={c.v} placeholder="value" onChange={e => updC(i, { v: e.target.value })} />
            <IconBtn ic="x" title="Remove condition" onClick={() => setConds(conds.filter((_, j) => j !== i))} />
          </div>)}</div>
          <div><Btn sm ic="plus" className="mt" onClick={() => setConds([...conds, { f: M.conds[0]?.[0] || 'source', op: 'is', v: '', _k: key() }])}>Add condition</Btn></div>
          <label className="chk mt"><input type="checkbox" checked={au.stop_on_reply} onChange={e => set({ stop_on_reply: e.target.checked })} /> Stop when the lead replies</label>
          <p className="hint">Placeholders: {M.vars}</p>
        </div>
        {!!runs.data?.rows.length && <div className="card"><div className="card-h"><h3>Recent runs</h3></div>
          {runs.data.rows.map(r => <div key={r.id} className="msg">
            <div className="msg-h"><Link className="lnk" to={'/leads/' + r.lead_id}>{r.lead}</Link><Badge c={RUN_BADGE[r.status] || 'gray'}>{r.status}</Badge><small>{ago(r.created_at)}</small></div>
            <RunLog log={r.log} tz={tz} />
          </div>)}
        </div>}
      </div>
      <div className="col">
        <div className="card">
          <div className="card-h"><h3><Hi n="bot" c="violet" />Then do this</h3>
            <div className={'dd' + (dd ? ' open' : '')} ref={ddRef}><Btn v="n" sm ic="plus" onClick={() => setDd(!dd)}>Add step</Btn>
              <div className="ddm">{M.actions.map(a => <button key={a.key} type="button" onClick={() => { setSteps([...steps, { ...newStep(a.key), _k: key() }]); setDd(false) }}><Icon n={a.ic} />{a.label}</button>)}</div>
            </div>
          </div>
          <div className="au-steps">{steps.map((s, i) => { const a = AC[s.t]; if (!a) return null; return <div key={s._k} className="au-st" data-t={s.t}>
            <Hi n={a.ic} c={CC[a.c] || 'gray'} />
            <div className="grow"><b>{a.label}</b>{body(s, i)}</div>
            <div className="au-mv"><button type="button" className="ib" title="Move up" onClick={() => move(i, -1)}>↑</button><button type="button" className="ib" title="Move down" onClick={() => move(i, 1)}>↓</button><IconBtn ic="trash" title="Remove" onClick={() => setSteps(steps.filter((_, j) => j !== i))} /></div>
          </div> })}</div>
          {steps.length === 0 && <p className="muted sm">No steps yet – click <b>Add step</b>. Tip: start with a WhatsApp message, then <i>Wait</i>, then a follow-up.</p>}
        </div>
      </div>
    </div>
  </form>
}
