import { Link, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Btn, Icon, Spinner, act, toast, usePageTitle } from '@/components/ui'
import { Flow, Meta, Step } from './shared'

interface Recipe { key: string; name: string; desc: string; ic: string; c: string; trig: string; trig_value: string; steps: Step[]; added: boolean }

export default function Recipes() {
  usePageTitle('Automation recipes', 'Ready-made flows – add one, review its steps, then switch it on')
  const nav = useNavigate(), qc = useQueryClient()
  const q = useQuery({ queryKey: ['automation-recipes'], queryFn: () => api.get<{ rows: Recipe[] }>('/automations/recipes') })
  const meta = useQuery({ queryKey: ['meta', 'automation'], queryFn: () => api.get<Meta>('/meta/automation'), staleTime: 60_000 })
  if (!q.data || !meta.data) return <Spinner />
  const T = Object.fromEntries(meta.data.triggers.map(t => [t.key, t.label]))
  const use = async (r: Recipe) => {
    const res = await act(() => api.post<{ id: number; message: string }>('/automations/recipes/' + r.key))
    if (!res) return
    toast(res.message)
    qc.invalidateQueries({ queryKey: ['automations'] })
    qc.invalidateQueries({ queryKey: ['automation-recipes'] })
    nav('/automations/' + res.id)
  }
  return <>
    <Link className="back" to="/automations">← Automations</Link>
    <div className="rcg mt">{q.data.rows.map(r => <div key={r.key} className={'card rcp3 c-' + r.c}>
      <div className="rcp3-h"><span className="hi"><Icon n={r.ic} /></span>
        <div className="grow"><b>{r.name}</b><small><Icon n="zap" />When {(T[r.trig] || r.trig).toLowerCase()}</small></div>
        {r.added && <span className="bdg c-green sm">Added</span>}
      </div>
      <p>{r.desc}</p>
      <Flow steps={r.steps} actions={meta.data!.actions} max={20} />
      <div className="rcp3-f"><Btn sm v="p" ic="plus" onClick={() => use(r)}>Use this recipe</Btn></div>
    </div>)}</div>
  </>
}
