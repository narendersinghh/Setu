// Settings → CRM → Custom fields (v1 settings.php tab "fields").
import { FormEvent, useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Btn, Hi, Icon, Spinner, act, confirmDo } from '@/components/ui'
import { CField } from './common'

const BUILTIN = ['Contact person', 'Business', 'Phone', 'Alt. phone', 'Email', 'Website', 'City', 'Address', 'Category', 'Service', 'Deal value', 'Status', 'Owner', 'Follow-up', 'Tags', 'Notes', 'Source']

export default function FieldsTab() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['crm-fields'], queryFn: () => api.get<{ fields: CField[]; types: [string, string][] }>('/fields') })
  const [rows, setRows] = useState<CField[]>([])
  const [n, setN] = useState({ label: '', fkey: '', type: 'text', options: '', required: false })
  useEffect(() => { if (q.data) setRows(q.data.fields) }, [q.data])
  if (!q.data) return <Spinner />
  const types = q.data.types
  const reload = () => { qc.invalidateQueries({ queryKey: ['crm-fields'] }); qc.invalidateQueries({ queryKey: ['crm-meta'] }) }
  const upd = (id: number, x: Partial<CField>) => setRows(r => r.map(f => f.id === id ? { ...f, ...x } : f))
  const save = async (e: FormEvent) => { e.preventDefault(); if (await act(() => api.put('/fields', { fields: rows }), 'Fields saved.')) reload() }
  const del = async (f: CField) => { if (confirmDo('Delete this field?') && await act(() => api.del('/fields/' + f.id), 'Field deleted (existing values stay in the lead data).')) reload() }
  const add = async (e: FormEvent) => { e.preventDefault(); if (await act(() => api.post('/fields', n), 'Field added.')) { setN({ label: '', fkey: '', type: 'text', options: '', required: false }); reload() } }
  return <>
    <div className="card"><div className="card-h"><h3><Hi n="list" c="green" />Built-in lead fields</h3></div>
      <div className="chips mb0">{BUILTIN.map(x => <a key={x} className="c-gray" style={{ paddingLeft: 12, cursor: 'default' }}>{x}</a>)}</div></div>
    <form className="card p0" onSubmit={save}><div className="card-h"><h3><Hi n="plus" c="blue" />Your custom fields</h3>{rows.length > 0 && <Btn sm v="n" type="submit" ic="check">Save changes</Btn>}</div>
      {!rows.length ? <div className="empty sm"><Icon n="list" /><p>No custom fields yet – add one below.</p></div> :
        <div className="tw"><table className="tbl"><thead><tr><th>Label</th><th>Key (for forms / webhooks)</th><th>Type</th><th>Dropdown options</th><th>Required</th><th>Order</th><th></th></tr></thead>
          <tbody>{rows.map(f => <tr key={f.id}>
            <td><input value={f.label} onChange={e => upd(f.id, { label: e.target.value })} /></td>
            <td><code>{f.fkey}</code></td>
            <td><select value={f.type} onChange={e => upd(f.id, { type: e.target.value })}>{types.map(([k, lb]) => <option key={k} value={k}>{lb}</option>)}</select></td>
            <td><textarea rows={2} placeholder="one per line" value={f.options} onChange={e => upd(f.id, { options: e.target.value })} /></td>
            <td><input type="checkbox" checked={f.required} onChange={e => upd(f.id, { required: e.target.checked })} /></td>
            <td><input type="number" className="sm-in" value={f.sort} onChange={e => upd(f.id, { sort: Number(e.target.value) })} /></td>
            <td className="act"><button type="button" className="ib red" title="Delete" onClick={() => del(f)}><Icon n="trash" /></button></td>
          </tr>)}</tbody></table></div>}
    </form>
    <form className="card" onSubmit={add}><div className="card-h"><h3><Hi n="plus" c="violet" />Add a field</h3></div>
      <div className="fg3"><label>Label<input required placeholder="e.g. Number of branches" value={n.label} onChange={e => setN({ ...n, label: e.target.value })} /></label>
        <label>Key (optional)<input placeholder="auto from label" value={n.fkey} onChange={e => setN({ ...n, fkey: e.target.value })} /></label>
        <label>Type<select value={n.type} onChange={e => setN({ ...n, type: e.target.value })}>{types.map(([k, lb]) => <option key={k} value={k}>{lb}</option>)}</select></label></div>
      <label>Dropdown options (one per line)<textarea rows={3} value={n.options} onChange={e => setN({ ...n, options: e.target.value })} /></label>
      <label className="chk"><input type="checkbox" checked={n.required} onChange={e => setN({ ...n, required: e.target.checked })} /> Required</label>
      <div className="card-f"><Btn v="p" type="submit" ic="plus">Add field</Btn></div></form>
  </>
}
