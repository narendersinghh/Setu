// Declarative settings form (same field model as v1 settings.php):
// [key, label, type?, options?, help?, showIf?] – showIf "a=1;b=x,y" shows the field only when all conditions match.
import { ReactNode, useEffect, useState, FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Btn, Field, Secret, toast, Spinner } from './ui'

export type FieldType = 'text' | 'textarea' | 'select' | 'secret' | 'number' | 'color' | 'file' | 'days' | 'email' | 'url'
export type FieldDef = [key: string, label: string, type?: FieldType, options?: Record<string, string> | null, help?: ReactNode, showIf?: string]

export function useSettings() {
  return useQuery({ queryKey: ['settings'], queryFn: () => api.get<Record<string, string>>('/settings') })
}

function visible(cond: string | undefined, v: Record<string, string>) {
  if (!cond) return true
  return cond.split(';').every(c => { const [k, vals] = c.split('='); return vals.split(',').includes(v[k] ?? '') })
}

const DAYS: [string, string][] = [['1', 'Mon'], ['2', 'Tue'], ['3', 'Wed'], ['4', 'Thu'], ['5', 'Fri'], ['6', 'Sat'], ['0', 'Sun']]

/** Renders fields bound to workspace settings and saves them with PUT /settings. */
export function SettingsForm({ fields, defaults = {}, children, extra, onSaved }: { fields: FieldDef[]; defaults?: Record<string, string>; children?: ReactNode; extra?: ReactNode; onSaved?: (v: Record<string, string>) => void }) {
  const q = useSettings()
  const qc = useQueryClient()
  const [v, setV] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (q.data) setV({ ...defaults, ...Object.fromEntries(Object.entries(q.data).filter(([, x]) => x !== '')) }) }, [q.data])
  if (q.isLoading) return <Spinner />
  const set = (k: string, x: string) => setV(s => ({ ...s, [k]: x }))
  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const out: Record<string, string> = {}
      fields.forEach(([k, , t]) => { if (t !== 'file') out[k] = v[k] ?? '' })
      const r = await api.put('/settings', out)
      qc.setQueryData(['settings'], r)
      qc.invalidateQueries({ queryKey: ['me'] })
      toast('Settings saved.')
      onSaved?.(v)
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const upload = async (k: string, f?: File) => {
    if (!f) return
    try {
      const r = await api.upload<{ url: string }>('/files', { file: f, setting: k })
      set(k, r.url)
      qc.invalidateQueries({ queryKey: ['settings'] })
      toast('Uploaded.')
    } catch (err: any) { toast(err.message) }
  }
  return <form onSubmit={save}>
    <div className="fg">
      {fields.map(([k, label, t = 'text', opts, help, cond]) => {
        if (!visible(cond, v)) return null
        const val = v[k] ?? ''
        let input: ReactNode
        switch (t) {
          case 'textarea': input = <textarea rows={3} value={val} onChange={e => set(k, e.target.value)} />; break
          case 'select': input = <select value={val} onChange={e => set(k, e.target.value)}>{Object.entries(opts || {}).map(([o, l]) => <option key={o} value={o}>{l}</option>)}</select>; break
          case 'secret': input = <Secret value={val} onChange={e => set(k, e.target.value)} placeholder={val ? '' : 'Not set'} />; break
          case 'color': input = <input type="color" value={val || '#000000'} onChange={e => set(k, e.target.value)} />; break
          case 'number': input = <input type="number" value={val} onChange={e => set(k, e.target.value)} />; break
          case 'days': {
            const on = new Set(val.split(',').filter(Boolean))
            input = <span className="row wrap">{DAYS.map(([d, l]) => <label key={d} className="chk"><input type="checkbox" checked={on.has(d)} onChange={e => { e.target.checked ? on.add(d) : on.delete(d); set(k, [...on].join(',')) }} /> {l}</label>)}</span>
            break
          }
          case 'file': input = <span className="row">{val && (/\.(png|jpe?g|webp|gif|svg)$/i.test(val) ? <img src={val} alt="" style={{ maxHeight: 40, background: '#222', borderRadius: 6, padding: 4 }} /> : <a className="lnk" href={val} target="_blank" rel="noreferrer">Current file</a>)}<input type="file" accept={opts ? Object.keys(opts).join(',') : undefined} onChange={e => upload(k, e.target.files?.[0])} />{val && <Btn sm v="ghost" ic="trash" onClick={() => set(k, '')}>Remove</Btn>}</span>; break
          default: input = <input type={t === 'email' ? 'email' : t === 'url' ? 'url' : 'text'} value={val} onChange={e => set(k, e.target.value)} />
        }
        return <Field key={k} label={label} hint={help}>{input}</Field>
      })}
    </div>
    {children}
    <div className="card-f"><Btn v="p" type="submit" ic="check" busy={busy}>Save settings</Btn>{extra}</div>
  </form>
}
