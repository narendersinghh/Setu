// Add a lead by hand or import a CSV (v1 lead_new.php).
import { FormEvent, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useCan, useMe } from '@/lib/session'
import { Btn, Hi, Icon, Spinner, toast, usePageTitle } from '@/components/ui'
import { CustomInput, STATUSES, localToISO, useMeta } from './common'

const MATCH: [string, string][] = [['Contact person', 'name, full name, first name, contact'], ['Business', 'company, business, organisation'], ['Phone', 'phone, mobile, whatsapp, contact number'], ['Email', 'email, e-mail'], ['City', 'city, location, town'], ['Service', 'service, interested in, product'], ['Deal value', 'budget, amount, value'], ['Notes', 'message, comments, requirement']]

export default function LeadNew() {
  usePageTitle('New lead', 'Add a lead by hand or import many from a spreadsheet')
  const can = useCan(), adm = can('settings')
  const [sp, setSp] = useSearchParams()
  const tab = sp.get('tab') === 'import' && adm ? 'import' : 'add'
  return <>
    <Link className="back" to="/leads">← Leads</Link>
    <div className="tabs mt"><a href="#" className={tab === 'add' ? 'on' : ''} onClick={e => { e.preventDefault(); setSp({}) }}><Icon n="user" />Add manually</a>{adm && <a href="#" className={tab === 'import' ? 'on' : ''} onClick={e => { e.preventDefault(); setSp({ tab: 'import' }) }}><Icon n="upload" />Import CSV</a>}</div>
    {tab === 'add' ? <AddForm /> : <ImportForm />}
  </>
}

const EMPTY: Record<string, string> = { contact_name: '', name: '', phone: '', email: '', alt_phone: '', website: '', city: '', category: '', address: '', message: '', notes: '', status: 'new', service: '', deal_value: '', follow_up_at: '', tags: '' }

function AddForm() {
  const me = useMe(), can = useCan(), adm = can('settings')
  const meta = useMeta().data
  const go = useNavigate(), qc = useQueryClient()
  const [v, setV] = useState<Record<string, string>>({ ...EMPTY, assigned_to: String(me.user.id) })
  const [cf, setCf] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  if (!meta) return <Spinner />
  const set = (k: string) => (e: { target: { value: string } }) => setV(s => ({ ...s, [k]: e.target.value }))
  const inp = (k: string, label: string, { full, ...p }: Record<string, any> = {}) => <label className={full ? 'full' : ''}>{label}<input name={k} value={v[k]} onChange={set(k)} {...p} /></label>
  const submit = async (e: FormEvent, another = false) => {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api.post<{ id: number; message: string }>('/leads', { ...v, follow_up_at: localToISO(v.follow_up_at), custom: cf })
      toast('Lead added.')
      qc.invalidateQueries({ queryKey: ['leads'] }); qc.invalidateQueries({ queryKey: ['nav'] })
      if (another) { setV({ ...EMPTY, assigned_to: v.assigned_to }); setCf({}) } else go('/leads/' + r.id)
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  return <form className="g3r" onSubmit={e => submit(e)}>
    <div className="col">
      <div className="card"><div className="sec"><Hi n="user" c="indigo" /><h3>Contact <small>– who are you talking to?</small></h3></div><div className="fg">
        {inp('contact_name', 'Contact person', { placeholder: 'Full name', autoFocus: true })}{inp('name', 'Business / company', { placeholder: 'Company name' })}
        {inp('phone', 'Phone / WhatsApp', { type: 'tel', placeholder: '+91 98xxx xxxxx' })}{inp('email', 'Email', { type: 'email', placeholder: 'name@company.com' })}
        {inp('alt_phone', 'Alternate phone', { type: 'tel' })}{inp('website', 'Website', { placeholder: 'example.com' })}</div></div>
      <div className="card"><div className="sec"><Hi n="pin" c="teal" /><h3>Business details</h3></div><div className="fg">
        {inp('city', 'City')}{inp('category', 'Category / industry', { placeholder: 'e.g. Dental clinic' })}{inp('address', 'Address', { full: true })}</div>
        {meta.fields.length > 0 && <><div className="sec mt"><Hi n="list" c="violet" /><h3>More fields</h3>{adm && <Link className="lnk sm" style={{ marginLeft: 'auto' }} to="/settings/fields">Manage fields</Link>}</div>
          <div className="fg">{meta.fields.map(f => <label key={f.id} className={f.type === 'textarea' ? 'full' : ''}>{f.label}{f.required ? ' *' : ''}<CustomInput f={f} value={cf[f.fkey] || ''} onChange={x => setCf(s => ({ ...s, [f.fkey]: x }))} /></label>)}</div></>}
      </div>
      <div className="card"><div className="sec"><Hi n="msg" c="amber" /><h3>Enquiry &amp; notes</h3></div>
        <label>What did they ask for?<textarea rows={3} value={v.message} onChange={set('message')} placeholder="Their requirement in their words" /></label>
        <label className="mb0">Internal notes<textarea rows={3} value={v.notes} onChange={set('notes')} placeholder="Only your team sees this" /></label></div>
    </div>
    <div className="col"><div className="card" style={{ position: 'sticky', top: 86 }}><div className="sec"><Hi n="target" c="green" /><h3>Deal</h3></div>
      <label>Status<select value={v.status} onChange={set('status')}>{STATUSES.map(s => <option key={s.key} value={s.key}>{s.label}</option>)}</select></label>
      <label>Service interested in<input value={v.service} onChange={set('service')} list="svc" /><datalist id="svc">{(meta.services || []).map(s => <option key={s} value={s} />)}</datalist></label>
      <label>Deal value ({meta.currency_symbol})<input type="number" step="0.01" min="0" value={v.deal_value} onChange={set('deal_value')} /></label>
      {adm && meta.users && <label>Owner<select value={v.assigned_to} onChange={set('assigned_to')}><option value="">Unassigned</option>{meta.users.map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select></label>}
      <label>Follow-up<input type="datetime-local" value={v.follow_up_at} onChange={set('follow_up_at')} /></label>
      <label>Tags<input value={v.tags} onChange={set('tags')} placeholder="hot, referral" /><small>Separate with commas. Tags can start automations.</small></label>
      <div className="card-f"><Btn v="p" type="submit" ic="check" busy={busy}>Save lead</Btn><Btn ic="plus" disabled={busy} onClick={e => submit(e as unknown as FormEvent, true)}>Save &amp; add another</Btn></div>
    </div></div>
  </form>
}

function ImportForm() {
  const go = useNavigate(), qc = useQueryClient()
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!file) return toast('Choose a CSV file.')
    setBusy(true)
    try {
      const r = await api.upload<{ message: string }>('/leads/import', { csv: file })
      toast(r.message)
      qc.invalidateQueries({ queryKey: ['leads'] }); qc.invalidateQueries({ queryKey: ['nav'] })
      go('/leads?src=import')
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  return <div className="g3r">
    <form className="card" onSubmit={submit}><div className="sec"><Hi n="upload" c="blue" /><h3>Import leads from a CSV file</h3></div>
      <label>CSV file<input type="file" accept=".csv,text/csv" required onChange={e => setFile(e.target.files?.[0] || null)} /><small>Excel / Google Sheets → File → Download / Save as → CSV.</small></label>
      <div className="card-f"><Btn v="p" type="submit" ic="upload" busy={busy}>Import</Btn><Link className="btn" to="/leads">Cancel</Link></div></form>
    <div className="card"><div className="sec"><Hi n="book" c="amber" /><h3>How columns are matched</h3></div><p className="sm">The first row must contain column names. They are matched automatically:</p>
      <table className="tbl sm"><tbody>{MATCH.map(([k, x]) => <tr key={k}><td><b>{k}</b></td><td className="muted">{x}</td></tr>)}</tbody></table>
      <p className="hint">Your custom field keys are matched too. Other columns are kept in the lead's notes. Leads with the same phone or email in the last 30 days are merged, not duplicated.</p></div>
  </div>
}
