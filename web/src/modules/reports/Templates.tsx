// Templates & report design: AI report prompts, outreach messages (email / WhatsApp), illustrations
// and the design preview (v1 app/pages/templates.php).
import { FormEvent, useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ago } from '@/lib/format'
import { Badge, Btn, Card, Empty, Field, Hi, Icon, Spinner, Tabs, confirmDo, toast, usePageTitle } from '@/components/ui'
import { Asset, AssetsResp, Prompt, copyText } from './common'
import { BrandName, useBrand } from '@/lib/theme'

interface TypesResp { types: { key: string; label: string; help: string }[]; vars: string[]; ai_vars: string[] }

function useTypes() { return useQuery({ queryKey: ['prompt-types'], queryFn: () => api.get<TypesResp>('/prompt-types') }) }
function useAssets() { return useQuery({ queryKey: ['report-assets'], queryFn: () => api.get<AssetsResp>('/assets') }) }

/** Full page (route /templates). */
export function TemplatesPage() {
  usePageTitle('Templates & Design', 'AI report prompts, messages and the look of your PDF reports')
  return <Templates />
}

/** Templates & report design (also the "Templates & report design" settings tab). */
export default function Templates() {
  const [sp, setSp] = useSearchParams()
  const types = useTypes()
  if (!types.data) return <Spinner />
  const keys = types.data.types.map(x => x.key)
  let t = sp.get('t') || 'report_website'
  if (t !== 'design' && !keys.includes(t)) t = 'report_website'
  const go = (k: string, id?: string | number) => { const n = new URLSearchParams(); n.set('t', k); if (id !== undefined) n.set('id', String(id)); setSp(n) }
  return <>
    <Tabs value={t} onChange={k => go(k)} tabs={[...types.data.types.map(x => ({ k: x.key, label: x.label })), { k: 'design', label: <><Icon n="image" />Report design</> }]} />
    {t === 'design' ? <Design /> : <PromptEditor t={t} types={types.data} id={sp.get('id')} go={go} />}
  </>
}

// ------------------------------------------------------------------ prompts & messages

function PromptEditor({ t, types, id, go }: { t: string; types: TypesResp; id: string | null; go: (k: string, id?: string | number) => void }) {
  const qc = useQueryClient()
  const list = useQuery({ queryKey: ['prompts', t], queryFn: () => api.get<Prompt[]>('/prompts', { type: t }) })
  const info = types.types.find(x => x.key === t)!
  const isAI = t.startsWith('report_'), isMail = t.startsWith('email')
  const vars = isAI ? [...types.vars, ...types.ai_vars] : types.vars
  const rows = list.data || []
  const cur: Prompt | null = id === 'new' ? { id: 0, type: t, name: '', subject: '', content: '', is_active: false, is_default: false, updated_at: '' } : rows.find(p => String(p.id) === id) || rows[0] || null
  const [f, setF] = useState<{ name: string; subject: string; content: string; use: boolean }>({ name: '', subject: '', content: '', use: false })
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (cur) setF({ name: cur.name, subject: cur.subject, content: cur.content, use: cur.is_active }) }, [cur?.id, cur?.updated_at, t])
  const refresh = () => qc.invalidateQueries({ queryKey: ['prompts'] })
  const save = async (e: FormEvent) => {
    e.preventDefault()
    if (!cur) return
    setBusy(true)
    try {
      const r = cur.id ? await api.put('/prompts/' + cur.id, f) : await api.post('/prompts', { ...f, type: t })
      toast(r.message || 'Saved.')
      await refresh()
      go(t, r.prompt.id)
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const call = async (fn: () => Promise<any>, then?: (r: any) => void) => {
    try { const r = await fn(); if (r?.message) toast(r.message); await refresh(); then?.(r) } catch (err: any) { toast(err.message) }
  }
  return <div className="g3">
    <div className="col">
      <Card title={info.label} actions={<Btn sm v="y" ic="plus" onClick={() => go(t, 'new')}>New</Btn>}>
        <p className="hint mt0">{info.help}</p>
        {!list.data ? <Spinner /> : <div className="plist">{rows.map(p => <a key={p.id} href="#" onClick={e => { e.preventDefault(); go(t, p.id) }} className={'pi' + (cur && cur.id === p.id ? ' on' : '')}>
          <b>{p.name}</b><span>{p.is_active ? <Badge c="green">In use</Badge> : <Badge>Saved</Badge>}<small>{ago(p.updated_at)}</small></span></a>)}</div>}
        <Btn sm ic="refresh" className="mt" onClick={() => confirmDo('Add a fresh copy of the default for this type?') && call(() => api.post('/prompts/reset', { type: t }), r => go(t, r.id))}>Restore default as new</Btn>
      </Card>
      <Card title="Placeholders">
        <p className="hint mt0">Click to copy. They are filled in for every business.</p>
        <div className="vars">{vars.map(v => <code key={v} onClick={() => copyText('{{' + v + '}}')}>{'{{' + v + '}}'}</code>)}</div>
        {isAI ? <p className="hint mt">Your prompt guides the tone and focus. The AI always answers in a fixed structure (summary, market stats with sources, findings, opportunity, plan, quick wins). <BrandName /> then designs the landscape PDF – cover, score gauges, charts, competitor graphs, illustrations and your branding – automatically.</p>
          : <p className="hint mt">Keep the first message simple – no fees. The PDF is attached to the email and WhatsApp message itself, so no link is needed.</p>}
      </Card>
    </div>
    <div className="col">
      {!cur ? <div className="card empty"><Icon n="msg" c="eic" /><p>Select or create a prompt.</p></div> : <>
        <form className="card frm" onSubmit={save}>
          <div className="card-h"><h3>{cur.id ? 'Edit' : 'New'} {isAI ? 'AI prompt' : 'message'}</h3>{!!cur.id && <span>{cur.is_active ? <Badge c="green">In use</Badge> : <Badge>Saved</Badge>}</span>}</div>
          <Field label="Name"><input required value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          {isMail && <Field label="Email subject"><input value={f.subject} onChange={e => setF({ ...f, subject: e.target.value })} /></Field>}
          <Field label={isAI ? 'Prompt sent to the AI' : 'Message text'}><textarea className="mono" rows={isAI ? 26 : 14} required value={f.content} onChange={e => setF({ ...f, content: e.target.value })} /></Field>
          <label className="chk"><input type="checkbox" checked={f.use} onChange={e => setF({ ...f, use: e.target.checked })} /> Use this {isAI ? 'prompt' : 'message'} (in use)</label>
          <div className="row"><Btn v="y" type="submit" ic="check" busy={busy}>Save</Btn></div>
        </form>
        {!!cur.id && <div className="row">
          {!cur.is_active && <Btn ic="check" onClick={() => call(() => api.post('/prompts/' + cur.id + '/use'))}>Set in use</Btn>}
          <Btn ic="copy" onClick={() => call(() => api.post('/prompts/' + cur.id + '/duplicate'), r => go(t, r.id))}>Duplicate</Btn>
          {!cur.is_active && <Btn v="r" ic="trash" onClick={() => confirmDo('Delete this prompt?') && call(() => api.del('/prompts/' + cur.id), () => go(t))}>Delete</Btn>}
        </div>}
        {isAI && !!cur.id && <PromptArt pid={cur.id} />}
      </>}
    </div>
  </div>
}

/** Illustrations that appear only in reports written with one prompt. */
function PromptArt({ pid }: { pid: number }) {
  const qc = useQueryClient()
  const a = useAssets()
  const mine = (a.data?.assets || []).filter(x => x.prompt_id === pid)
  const [place, setPlace] = useState('any')
  const ref = useRef<HTMLInputElement>(null)
  const up = async (e: FormEvent) => {
    e.preventDefault()
    const files = ref.current?.files
    if (!files?.length) return toast('Choose one or more images.')
    const fd = new FormData()
    for (const f of Array.from(files)) fd.append('art', f)
    fd.append('placement', place)
    fd.append('prompt_id', String(pid))
    try { const r = await api.post('/assets', fd); toast(r.message); ref.current!.value = ''; qc.invalidateQueries({ queryKey: ['report-assets'] }) } catch (err: any) { toast(err.message) }
  }
  return <form className="card frm mt" onSubmit={up}>
    <div className="card-h"><h3><Hi n="image" c="amber" />Illustrations for this prompt</h3><Link className="lnk" to="?t=design">All illustrations</Link></div>
    {mine.length ? <div className="doodles">{mine.map(x => <div key={x.id} className="dd-i"><img src={x.file} alt="" /><b>{x.name}</b><small>{a.data?.places.find(p => p.key === x.placement)?.label}</small></div>)}</div>
      : <p className="hint mt0">Upload company-specific doodles or cartoons that should appear only in reports written with this prompt.</p>}
    <div className="row mt">
      <input ref={ref} type="file" accept=".png,.jpg,.jpeg,.webp,.svg" multiple required />
      <select value={place} onChange={e => setPlace(e.target.value)} style={{ maxWidth: 200 }}>{(a.data?.places || []).map(p => <option key={p.key} value={p.key}>{p.label}</option>)}</select>
      <Btn v="n" type="submit" ic="upload">Upload</Btn>
    </div>
  </form>
}

// ------------------------------------------------------------------ report design

function Design() {
  const qc = useQueryClient()
  const a = useAssets()
  const ref = useRef<HTMLInputElement>(null)
  const [up, setUp] = useState({ name: '', placement: 'cover', prompt_id: '' })
  const [pv, setPv] = useState('0')
  const [busy, setBusy] = useState(false)
  if (!a.data) return <Spinner />
  const d = a.data
  const placeLabel = (k: string) => d.places.find(p => p.key === k)?.label || k
  const upload = async (e: FormEvent) => {
    e.preventDefault()
    const files = ref.current?.files
    if (!files?.length) return toast('Choose one or more images.')
    const fd = new FormData()
    for (const f of Array.from(files)) fd.append('art', f)
    fd.append('name', up.name)
    fd.append('placement', up.placement)
    fd.append('prompt_id', up.prompt_id)
    setBusy(true)
    try { const r = await api.post('/assets', fd); toast(r.message); ref.current!.value = ''; setUp({ ...up, name: '' }); qc.invalidateQueries({ queryKey: ['report-assets'] }) } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const remove = async (x: Asset) => {
    if (!confirmDo('Remove this illustration?')) return
    try { const r = await api.del('/assets/' + x.id); toast(r.message); qc.invalidateQueries({ queryKey: ['report-assets'] }) } catch (err: any) { toast(err.message) }
  }
  const open = (format = '') => window.open('/api/reports/preview?prompt_id=' + pv + (format ? '&format=' + format : ''), '_blank')
  return <div className="g3">
    <div className="col">
      <form className="card frm" onSubmit={upload}>
        <div className="card-h"><h3><Hi n="upload" c="amber" />Upload your illustrations</h3></div>
        <p className="hint mt0">Add your own doodles, cartoons, mascots or photos. They replace the built-in artwork in the matching report section. PNG with a transparent background looks best (about 800×520 px).</p>
        <Field label="Images"><input ref={ref} type="file" accept=".png,.jpg,.jpeg,.webp,.svg" multiple required /></Field>
        <Field label="Name (optional)"><input value={up.name} placeholder="e.g. Mascot waving" onChange={e => setUp({ ...up, name: e.target.value })} /></Field>
        <Field label="Use in section"><select value={up.placement} onChange={e => setUp({ ...up, placement: e.target.value })}>{d.places.map(p => <option key={p.key} value={p.key}>{p.label}</option>)}</select></Field>
        <Field label="Use with prompt" hint="Pick a prompt to give it its own visual style."><select value={up.prompt_id} onChange={e => setUp({ ...up, prompt_id: e.target.value })}><option value="">All report prompts</option>{d.prompts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></Field>
        <Btn v="y" type="submit" ic="upload" busy={busy}>Upload</Btn>
      </form>
      <Card title={<><Hi n="eye" c="blue" />Preview the design</>}>
        <p className="hint mt0">Re-renders your latest report (or sample content) with the current colours, fonts and illustrations – no AI cost.</p>
        <div className="frm">
          <Field label="Illustration set"><select value={pv} onChange={e => setPv(e.target.value)}><option value="0">Default (all prompts)</option>{d.prompts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></Field>
          <div className="row"><Btn v="n" ic="file" onClick={() => open()}>Open preview PDF</Btn><Btn ic="globe" onClick={() => open('html')}>Web preview</Btn></div>
        </div>
        <p className="hint">Colours, fonts and logo: <Link className="lnk" to="/settings/branding">Settings → Branding</Link></p>
      </Card>
    </div>
    <div className="col">
      <Card title="Your illustrations" actions={<span className="muted">{d.assets.length}</span>}>
        {!d.assets.length ? <p className="muted sm">None yet – the built-in illustrations below are used.</p> :
          <div className="doodles">{d.assets.map(x => <div key={x.id} className="dd-i"><img src={x.file} alt="" /><b>{x.name}</b><small>{placeLabel(x.placement)} · {x.prompt_name || 'all prompts'}</small><Btn xs ic="trash" className="mt" onClick={() => remove(x)}>Remove</Btn></div>)}</div>}
      </Card>
      <Card title="Built-in illustrations" actions={<span className="muted sm">in your brand colours</span>}>
        {!d.builtin.length ? <Empty title="No illustrations" /> : <div className="doodles">{d.builtin.map(b => <div key={b.key} className="dd-i"><img src={b.uri} alt="" /><b>{b.label}</b></div>)}</div>}
      </Card>
    </div>
  </div>
}
