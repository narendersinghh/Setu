import { FormEvent, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useMe, useCan, Role } from '@/lib/session'
import { ago, initial } from '@/lib/format'
import { Alert, Badge, Btn, Card, Field, Hi, Kpi, Modal, Secret, Spinner, Switch, toast, usePageTitle, confirmDo } from '@/components/ui'

interface Member { id: number; name: string; email: string; phone: string; role: Role; active: boolean; last_login: string | null; shared: boolean; open_n: number; won_n: number }

const RC: Record<Role, string> = { super: 'violet', admin: 'blue', sales: 'green' }
const PERM: Record<Role, string[]> = {
  super: ['Switch modules on/off', 'Manage all users incl. super admins', 'All settings, API keys & usage', 'Everything admins can do'],
  admin: ['All leads, assign owners, import/export', 'Prospecting: scraper, audits, reports', 'Automations, templates, AI & settings', 'Add admins and sales people'],
  sales: ['Own leads + unassigned leads', 'Add leads, notes, status, follow-ups', 'Book appointments, log calls & replies', 'No settings, exports or deletes'],
}

export function RoleBadge({ r }: { r: Role }) { const me = useMe(); const x = me.roles[r]; return <Badge c={x?.[1] || 'gray'}>{x?.[0] || r}</Badge> }

export default function Team() {
  usePageTitle('Team & Roles', 'Who can use Setu and what each person can do')
  const me = useMe(), can = useCan()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['team'], queryFn: () => api.get<{ members: Member[]; me: number }>('/team') })
  const [edit, setEdit] = useState<Partial<Member> & { password?: string; release?: boolean } | null>(null)
  if (!q.data) return <Spinner />
  const rows = q.data.members
  const act = rows.filter(u => u.active)
  const cnt = (r: Role) => act.filter(u => u.role === r).length
  const save = async (e: FormEvent) => {
    e.preventDefault()
    if (!edit) return
    try {
      const r = edit.id ? await api.put('/team/' + edit.id, edit) : await api.post('/team', { ...edit, active: true })
      toast(r.message || 'Saved.')
      setEdit(null)
      qc.invalidateQueries({ queryKey: ['team'] })
    } catch (err: any) { toast(err.message) }
  }
  const remove = async (u: Member) => {
    if (!confirmDo('Remove ' + u.name + ' from your team? Their leads become unassigned.')) return
    try { await api.del('/team/' + u.id); toast('User removed. Their leads are now unassigned.'); qc.invalidateQueries({ queryKey: ['team'] }) } catch (err: any) { toast(err.message) }
  }
  const roles: Role[] = can('super_users') ? ['super', 'admin', 'sales'] : ['admin', 'sales']
  return <>
    <div className="stats">
      <Kpi label="Active users" value={act.length} ic="users" c="indigo" foot={rows.length + ' in total'} />
      <Kpi label="Super admins" value={cnt('super')} ic="shield" c="violet" foot="full control" />
      <Kpi label="Admins / managers" value={cnt('admin')} ic="user" c="blue" foot="run the team" />
      <Kpi label="Sales people" value={cnt('sales')} ic="target" c="green" foot="work their leads" />
    </div>
    <div className="g31">{(['super', 'admin', 'sales'] as Role[]).map(k => <div key={k} className={'card c-' + RC[k]}><div className="au-card"><Hi n={k === 'super' ? 'shield' : k === 'admin' ? 'user' : 'target'} c={RC[k]} /><div><b>{me.roles[k][0]}</b><p className="hint mt0" style={{ marginTop: 2 }}>{me.roles[k][2]}</p></div></div><ul className="ul">{PERM[k].map(p => <li key={p}>{p}</li>)}</ul></div>)}</div>
    <Card p0 title="Team members" ic="users" c="blue" actions={<Btn v="p" sm ic="plus" onClick={() => setEdit({ role: 'sales', active: true })}>Add team member</Btn>}>
      <div className="tw"><table className="tbl"><thead><tr><th className="sn">#</th><th>Person</th><th>Role</th><th>Status</th><th className="num">Open leads</th><th className="num">Won</th><th>Last sign-in</th><th></th></tr></thead>
        <tbody>{rows.map((u, i) => { const mine = u.id === q.data!.me; const editable = !mine && (can('super_users') || u.role !== 'super'); return <tr key={u.id} style={{ opacity: u.active ? 1 : .55 }}>
          <td className="sn">{i + 1}</td>
          <td><div className="who"><span className="av">{initial(u.name)}</span><div className="biz"><b>{u.name}{mine && <> <span className="bdg c-gray sm">You</span></>}</b><small>{u.email}{u.phone && ' · ' + u.phone}</small></div></div></td>
          <td><RoleBadge r={u.role} /></td>
          <td>{u.active ? <Badge c="green" dot>Active</Badge> : <Badge>Deactivated</Badge>}</td>
          <td className="num"><Link className="lnk" to={'/leads?own=' + u.id}>{u.open_n}</Link></td>
          <td className="num">{u.won_n}</td>
          <td>{ago(u.last_login)}</td>
          <td className="row">{mine ? <Link className="btn btn-sm" to="/account">My account</Link> : editable && <><Btn sm ic="edit" onClick={() => setEdit({ ...u })}>Edit</Btn><Btn sm v="ghost" ic="trash" title="Remove" onClick={() => remove(u)} /></>}</td>
        </tr> })}</tbody></table></div>
    </Card>
    <Modal open={!!edit} onClose={() => setEdit(null)} title={edit?.id ? edit.name : 'Add team member'} ic="user" c="blue">
      {edit && <form onSubmit={save}>
        {edit.shared && <Alert>This person also uses Setu in another organisation, so only their role and access can be changed here.</Alert>}
        <div className="fg">
          <Field label="Name"><input required disabled={edit.shared} value={edit.name || ''} onChange={e => setEdit({ ...edit, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" required disabled={edit.shared} value={edit.email || ''} onChange={e => setEdit({ ...edit, email: e.target.value })} /></Field>
          <Field label="Phone"><input disabled={edit.shared} value={edit.phone || ''} onChange={e => setEdit({ ...edit, phone: e.target.value })} /></Field>
          <Field label="Role"><select value={edit.role} onChange={e => setEdit({ ...edit, role: e.target.value as Role })}>{roles.map(r => <option key={r} value={r}>{me.roles[r][0]}</option>)}</select></Field>
          {!edit.shared && <Field label={edit.id ? 'New password (optional)' : 'Password'} hint="8+ characters. If this email already has a Setu login, they keep their own password."><Secret minLength={8} value={edit.password || ''} onChange={e => setEdit({ ...edit, password: e.target.value })} /></Field>}
          {!!edit.id && <Field label="Access"><span className="row"><Switch checked={!!edit.active} onChange={v => setEdit({ ...edit, active: v })} />{edit.active ? 'Active' : 'Deactivated'}</span></Field>}
          {!!edit.id && !edit.active && <label className="chk"><input type="checkbox" checked={!!edit.release} onChange={e => setEdit({ ...edit, release: e.target.checked })} /> Unassign their open leads</label>}
        </div>
        <div className="md-f"><Btn onClick={() => setEdit(null)}>Cancel</Btn><Btn v="p" type="submit" ic="check">Save</Btn></div>
      </form>}
    </Modal>
  </>
}
