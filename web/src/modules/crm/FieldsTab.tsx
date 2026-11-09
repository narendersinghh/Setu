// Settings → CRM → Custom fields (v1 settings.php tab "fields"). Dropdown options are edited as chips; the value
// stays the newline-separated string the server stores.
import { FormEvent, useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Btn, ChipsInput, EmptyState, Hi, Icon, Spinner, Switch, act, confirmDo } from '@/components/ui'
import { CoffeeBot } from '@/components/Cartoons'
import { CField } from './common'

const BUILTIN = ['Contact person', 'Business', 'Phone', 'Alt. phone', 'Email', 'Website', 'City', 'Address', 'Category', 'Service', 'Deal value', 'Status', 'Owner', 'Follow-up', 'Tags', 'Notes', 'Source']
const TYPE_IC: Record<string, string> = { text: 'edit', textarea: 'file', number: 'activity', date: 'cal', select: 'list', checkbox: 'check', email: 'mail', phone: 'phone', url: 'link' }
const blank = { label: '', fkey: '', type: 'text', options: '', required: false }

export default function FieldsTab() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['crm-fields'], queryFn: () => api.get<{ fields: CField[]; types: [string, string][] }>('/fields') })
  const [rows, setRows] = useState<CField[]>([])
  const [n, setN] = useState(blank)
  const [dirty, setDirty] = useState(false)
  useEffect(() => { if (q.data) { setRows(q.data.fields); setDirty(false) } }, [q.data])
  if (!q.data) return <Spinner />
  const types = q.data.types
  const reload = () => { qc.invalidateQueries({ queryKey: ['crm-fields'] }); qc.invalidateQueries({ queryKey: ['crm-meta'] }) }
  const upd = (id: number, x: Partial<CField>) => { setRows(r => r.map(f => f.id === id ? { ...f, ...x } : f)); setDirty(true) }
  const save = async (e: FormEvent) => { e.preventDefault(); if (await act(() => api.put('/fields', { fields: rows }), 'Fields saved.')) reload() }
  const del = async (f: CField) => { if (confirmDo('Delete this field?') && await act(() => api.del('/fields/' + f.id), 'Field deleted (existing values stay in the lead data).')) reload() }
  const add = async (e: FormEvent) => { e.preventDefault(); if (await act(() => api.post('/fields', n), 'Field added.')) { setN(blank); reload() } }
  return <>
    <div className="card"><div className="card-h"><h3><Hi n="list" c="green" />Built-in lead fields</h3><span className="muted sm">Always available</span></div>
      <div className="chips mb0">{BUILTIN.map(x => <span key={x} className="fld-b">{x}</span>)}</div></div>
    <form className="card p0" onSubmit={save}><div className="card-h"><h3><Hi n="layers" c="blue" />Your custom fields</h3>{rows.length > 0 && <Btn sm v={dirty ? 'p' : 'n'} type="submit" ic="check">{dirty ? 'Save changes' : 'Saved'}</Btn>}</div>
      {!rows.length ? <EmptyState art={<CoffeeBot />} title="No custom fields yet">Add fields such as "Number of branches" or "Budget range" below – they show on every lead and in forms.</EmptyState> :
        <div className="fld-l">{rows.map(f => <div key={f.id} className="fld">
          <div className="fld-r">
            <span className="fld-i c-blue"><Icon n={TYPE_IC[f.type] || 'edit'} /></span>
            <label className="fld-lb">Label<input value={f.label} onChange={e => upd(f.id, { label: e.target.value })} /></label>
            <label className="fld-ty">Type<select value={f.type} onChange={e => upd(f.id, { type: e.target.value })}>{types.map(([k, lb]) => <option key={k} value={k}>{lb}</option>)}</select></label>
            <label className="fld-or">Order<input type="number" value={f.sort} onChange={e => upd(f.id, { sort: Number(e.target.value) })} /></label>
            <label className="fld-rq">Required<span className="row"><Switch checked={f.required} onChange={v => upd(f.id, { required: v })} /></span></label>
            <button type="button" className="ib red fld-x" title="Delete field" aria-label={'Delete ' + f.label} onClick={() => del(f)}><Icon n="trash" /></button>
          </div>
          <div className="fld-m"><span className="muted xs">Key for forms &amp; webhooks</span> <code>{f.fkey}</code></div>
          {f.type === 'select' && <div className="fld-o"><span className="fld-ol">Dropdown options</span><ChipsInput value={f.options} onChange={v => upd(f.id, { options: v })} label={f.label + ' options'} /></div>}
        </div>)}</div>}
    </form>
    <form className="card" onSubmit={add}><div className="card-h"><h3><Hi n="plus" c="violet" />Add a field</h3></div>
      <div className="fg3"><label>Label<input required placeholder="e.g. Number of branches" value={n.label} onChange={e => setN({ ...n, label: e.target.value })} /></label>
        <label>Key (optional)<input placeholder="auto from label" value={n.fkey} onChange={e => setN({ ...n, fkey: e.target.value })} /></label>
        <label>Type<select value={n.type} onChange={e => setN({ ...n, type: e.target.value })}>{types.map(([k, lb]) => <option key={k} value={k}>{lb}</option>)}</select></label></div>
      {n.type === 'select' && <div className="fld-o mb"><span className="fld-ol">Dropdown options</span><ChipsInput value={n.options} onChange={v => setN({ ...n, options: v })} label="New field options" /></div>}
      <label className="chk"><input type="checkbox" checked={n.required} onChange={e => setN({ ...n, required: e.target.checked })} /> Required</label>
      <div className="card-f"><Btn v="p" type="submit" ic="plus">Add field</Btn></div></form>
  </>
}
