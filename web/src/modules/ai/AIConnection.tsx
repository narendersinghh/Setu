// AI connection (v1 ai.php): provider, API key, model, custom endpoint, web research switch, test button.
import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Alert, Badge, Btn, Field, Hi, Secret, Spinner, Switch, toast } from '@/components/ui'

interface ProviderView { key: string; name: string; url: string; models: string; steps: string[]; note: string; suggest: string[]; has_key: boolean; model: string }
interface ConnView {
  provider: string; providers: ProviderView[]; research: boolean; custom_url: string; anthropic_base: string; research_cache_days: number; configured: boolean
  message?: string; test?: { name: string; secs: number; reply: string; model: string }
}

export default function AIConnection() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['ai-conn'], queryFn: () => api.get<ConnView>('/ai') })
  const [p, setP] = useState('anthropic')
  const [keys, setKeys] = useState<Record<string, string>>({})
  const [clear, setClear] = useState<Record<string, boolean>>({})
  const [models, setModels] = useState<Record<string, string>>({})
  const [url, setUrl] = useState('')
  const [base, setBase] = useState('')
  const [research, setResearch] = useState(true)
  const [busy, setBusy] = useState<'' | 'save' | 'test'>('')
  const [flash, setFlash] = useState<{ t: 'ok' | 'err'; m: React.ReactNode } | null>(null)
  const d = q.data
  useEffect(() => {
    if (!d) return
    setP(d.provider)
    setModels(Object.fromEntries(d.providers.map(x => [x.key, x.model || x.suggest[0] || ''])))
    setUrl(d.custom_url); setBase(d.anthropic_base); setResearch(d.research)
  }, [d])
  if (!d) return <Spinner />
  const P = d.providers.find(x => x.key === p) || d.providers[0]
  const act = d.providers.find(x => x.key === d.provider)

  const submit = async (e: FormEvent, test = false) => {
    e.preventDefault()
    setBusy(test ? 'test' : 'save'); setFlash(null)
    const body = { provider: p, key: keys[p] || '', clear: !!clear[p], model: models[p] || '', url, anthropic_base: base, research }
    try {
      const r = await (test ? api.post<ConnView>('/ai/test', body) : api.put<ConnView>('/ai', body))
      qc.setQueryData(['ai-conn'], r)
      qc.invalidateQueries({ queryKey: ['settings'] }); qc.invalidateQueries({ queryKey: ['me'] })
      setKeys(k => ({ ...k, [p]: '' })); setClear(c => ({ ...c, [p]: false }))
      if (r.test) setFlash({ t: 'ok', m: <><b>{r.test.name}</b> replied in {r.test.secs}s: “{r.test.reply}”</> })
      else toast(r.message || 'Saved.')
    } catch (err: any) {
      setFlash({ t: 'err', m: err.message })
      qc.invalidateQueries({ queryKey: ['ai-conn'] })
    } finally { setBusy('') }
  }

  return <>
    <form onSubmit={e => submit(e)}>
      {flash && <Alert t={flash.t}>{flash.m}</Alert>}
      {act?.has_key && <p className="mt0"><Badge c="green" dot>Using {act.name}</Badge></p>}
      <Field label="1. Choose a provider"><select value={p} onChange={e => { setP(e.target.value); setFlash(null) }}>
        {d.providers.map(x => <option key={x.key} value={x.key}>{x.name}{x.has_key ? ' ✓' : ''}</option>)}</select></Field>
      <p className="sm muted mt0">{P.note}{P.url && <> <a className="lnk" href={P.url} target="_blank" rel="noopener noreferrer">Get an API key ↗</a></>}</p>
      {p === 'custom' && <Field label="Base URL"><input value={url} onChange={e => setUrl(e.target.value)} placeholder="https://api.example.com/v1" /></Field>}
      <div className="fg">
        <label>2. API key<Secret value={keys[p] || ''} onChange={e => setKeys({ ...keys, [p]: e.target.value })} placeholder={P.has_key ? '•••••••• saved – type to replace' : 'Paste your API key'} />
          {P.has_key && <span className="chk sm" style={{ marginTop: 6 }}><input type="checkbox" checked={!!clear[p]} onChange={e => setClear({ ...clear, [p]: e.target.checked })} /> remove saved key</span>}</label>
        <label>3. Model<input value={models[p] || ''} onChange={e => setModels({ ...models, [p]: e.target.value })} list={'m-' + p} />
          <datalist id={'m-' + p}>{P.suggest.map(m => <option key={m} value={m} />)}</datalist>
          <small>Suggested: {P.suggest.join(', ') || 'any model your endpoint supports'}</small></label>
      </div>
      <details className="mb0"><summary className="lnk sm" style={{ cursor: 'pointer' }}>Step-by-step: how to get a key</summary><ol className="ol">{P.steps.map(s => <li key={s}>{s}</li>)}</ol></details>
      {p === 'anthropic' && <details className="mb0"><summary className="lnk sm" style={{ cursor: 'pointer' }}>Advanced: Claude API address</summary>
        <Field label="API address (proxy / gateway)" hint="Leave empty to use https://api.anthropic.com"><input value={base} onChange={e => setBase(e.target.value)} placeholder="https://api.anthropic.com" /></Field></details>}
      <label className="chk mt"><Switch checked={research} onChange={setResearch} /> Market research step for reports (live web search with Claude; model knowledge with other providers)</label>
      <p className="hint" style={{ margin: '-4px 0 0' }}>When off, the AI does no research at all – no research call, no web searches – and writes only from the audit data (faster and cheaper). When on, research is shared for {d.research_cache_days} days by businesses of the same kind in the same city, so it is not repeated for every report.</p>
      <div className="card-f"><Btn v="p" type="submit" ic="check" busy={busy === 'save'}>Save &amp; use</Btn><Btn ic="zap" busy={busy === 'test'} onClick={e => submit(e as unknown as FormEvent, true)}>Save &amp; test</Btn></div>
    </form>
    <div className="g2 mt">
      <div><div className="sec"><Hi n="activity" c="blue" /><h3>Connected providers</h3></div>
        {d.providers.filter(x => x.has_key || x.key === d.provider).map(x => <div className="li" key={x.key}>
          <span className={'avc c-' + (d.provider === x.key ? 'green' : 'gray')}>{x.name.charAt(0)}</span>
          <div className="grow"><b>{x.name}</b><small>{x.model || 'no model set'}</small></div>
          {d.provider === x.key ? <Badge c="green">In use</Badge> : <Badge c="teal">Key saved</Badge>}</div>)}</div>
      <div><div className="sec"><Hi n="book" c="amber" /><h3>How AI is used</h3></div>
        <ol className="ol mt0"><li>Setu audits the website and Google listing (no AI cost).</li>
          <li>{d.research ? 'The AI researches the market (once per business type & city), then writes the report from the audit data and your prompt.' : 'The AI writes the report from the audit data and your prompt (market research is off).'}</li>
          <li>Setu designs the branded PDF.</li></ol>
        <p className="hint">One report uses roughly 15–30k tokens. See costs in <Link className="lnk" to="/usage">API usage</Link>.</p></div>
    </div>
  </>
}

