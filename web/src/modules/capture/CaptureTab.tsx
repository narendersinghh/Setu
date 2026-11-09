// Settings → CRM → Lead capture (v1 settings.php 'capture' tab).
import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ago } from '@/lib/format'
import { Alert, Badge, Btn, Field, Hi, Icon, Secret, Spinner, Switch, confirmDo, toast } from '@/components/ui'

interface SrcType { key: string; label: string; desc: string; ic: string; c: string; hint: string }
interface KeyRow { key: string; label: string; sample: string; auto: string; auto_label: string }
interface Source {
  id: number; type: string; name: string; active: boolean; received: number; last_at: string | null; url: string
  last_payload: string; last_error: { at: string; msg: string } | null; keys: KeyRow[]; map: Record<string, string>
  cfg: { verify_token: string; google_key: string; thanks_url: string; thanks_title: string; thanks_text: string; page_token_set: boolean; app_secret_set: boolean }
  html: string; script: string
}
interface Data { types: SrcType[]; sources: Source[]; options: { v: string; l: string }[] }

async function copy(t: string) {
  try { await navigator.clipboard.writeText(t); toast('Copied.') } catch { toast('Select the text and copy it.') }
}

function CopyInput({ value }: { value: string }) {
  return <span className="copy"><input readOnly value={value} onFocus={e => e.target.select()} /><Btn sm ic="copy" onClick={() => copy(value)}>Copy</Btn></span>
}

export default function CaptureTab() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['capture'], queryFn: () => api.get<Data>('/capture') })
  const [names, setNames] = useState<Record<string, string>>({})
  if (!q.data) return <Spinner />
  const { types, sources } = q.data
  const reload = () => qc.invalidateQueries({ queryKey: ['capture'] })
  const add = async (e: FormEvent, type: string) => {
    e.preventDefault()
    try {
      const r = await api.post<{ id: number; message: string }>('/capture/sources', { type, name: names[type] || '' })
      toast(r.message)
      setNames(n => ({ ...n, [type]: '' }))
      await reload()
      setTimeout(() => document.getElementById('s' + r.id)?.scrollIntoView({ behavior: 'smooth' }), 100)
    } catch (err: any) { toast(err.message) }
  }
  return <>
    <div className="card">
      <div className="card-h"><h3><Hi n="plug" c="orange" />Capture leads automatically</h3></div>
      <p className="sm muted mt0">Every enquiry lands in <b>Leads</b> with its source, starts your automations and is de-duplicated by phone/email. Create <b>one source per form, ad account or tool</b> so you can see where leads come from.</p>
      <div className="g2 mb0 mt">{types.map(t => {
        const n = sources.filter(s => s.type === t.key).length
        return <form key={t.key} className={'srcc c-' + t.c} onSubmit={e => add(e, t.key)}>
          <span className="hi"><Icon n={t.ic} /></span>
          <div className="grow">
            <div className="row nw" style={{ gap: 8 }}><b>{t.label}</b>{n > 0 && <Badge c="green">{n} added</Badge>}</div>
            <p>{t.desc}</p>
            <div className="row nw" style={{ gap: 6 }}><input placeholder={'Name, e.g. ' + t.hint} value={names[t.key] || ''} onChange={e => setNames(x => ({ ...x, [t.key]: e.target.value }))} /><Btn sm v="n" ic="plus" type="submit">Add</Btn></div>
          </div>
        </form>
      })}</div>
    </div>
    <div className="card">
      <div className="card-h"><h3><Hi n="book" c="blue" />How field mapping works</h3></div>
      <ol className="ol mt0">
        <li>Your form sends its fields (for example <code>Name</code>, <code>Mobile</code>, <code>Which service?</code>) to the source URL.</li>
        <li>Setu matches common names automatically: <code>name / full name</code> → Contact person, <code>phone / mobile / whatsapp</code> → Phone, <code>email</code> → Email, <code>company</code> → Business, <code>city</code>, <code>service</code>, <code>budget</code> → Deal value, <code>message</code> → Enquiry. Your custom field keys match too.</li>
        <li>Anything it doesn't recognise is saved in the lead's enquiry notes – nothing is lost.</li>
        <li>After the first enquiry arrives (send a test from your form), each source below shows a <b>Field mapping</b> table with every field it received. Change any row to send that field where you want, then Save.</li>
      </ol>
    </div>
    {sources.map(s => <SourceCard key={s.id} s={s} data={q.data!} reload={reload} />)}
  </>
}

function SourceCard({ s, data, reload }: { s: Source; data: Data; reload: () => Promise<void> }) {
  const t = data.types.find(x => x.key === s.type) || data.types[data.types.length - 1]
  const [name, setName] = useState(s.name)
  const [active, setActive] = useState(s.active)
  const [cfg, setCfg] = useState<Record<string, string>>({})
  const [map, setMap] = useState<Record<string, string>>(s.map)
  const [busy, setBusy] = useState('')
  useEffect(() => { setName(s.name); setActive(s.active); setMap(s.map); setCfg({}) }, [s])
  const c = (k: keyof Source['cfg']) => cfg[k] ?? String(s.cfg[k] ?? '')
  const setC = (k: string, v: string) => setCfg(x => ({ ...x, [k]: v }))
  const run = async (what: string, fn: () => Promise<any>) => {
    setBusy(what)
    try { const r = await fn(); if (r?.message) toast(r.message); await reload() } catch (err: any) { toast(err.message) } finally { setBusy('') }
  }
  const save = (e?: FormEvent) => { e?.preventDefault(); return run('save', () => api.put('/capture/sources/' + s.id, { name, active, cfg, map })) }
  const test = () => run('test', () => api.post('/capture/sources/' + s.id + '/test'))
  const del = () => { if (confirmDo('Delete this source? Its URL will stop working.')) run('del', () => api.del('/capture/sources/' + s.id)) }
  const urlLabel = s.type === 'facebook' ? 'Callback URL' : s.type === 'google' ? 'Webhook URL' : 'Endpoint URL'
  return <form className="card" id={'s' + s.id} onSubmit={save}>
    <div className="card-h">
      <h3><Hi n={t.ic} c={t.c} />{s.name} <Badge>{t.label}</Badge></h3>
      <div className="row"><span className="muted sm">{s.received} leads{s.last_at && ' · last ' + ago(s.last_at)}</span><Switch checked={active} onChange={setActive} title="Active" /></div>
    </div>
    {s.last_error && <Alert t="err"><b>Last enquiry could not be saved</b> ({ago(s.last_error.at)}): {s.last_error.msg}</Alert>}
    <div className="fg">
      <Field label="Name"><input value={name} onChange={e => setName(e.target.value)} /></Field>
      <Field label={urlLabel}><CopyInput value={s.url} /></Field>
    </div>

    {s.type === 'facebook' && <>
      <div className="fg">
        <Field label="Verify token"><CopyInput value={s.cfg.verify_token} /></Field>
        <Field label="Page access token"><Secret value={cfg.page_token || ''} onChange={e => setC('page_token', e.target.value)} placeholder={s.cfg.page_token_set ? '•••••••• saved' : 'EAAG… (long-lived page token)'} /></Field>
        <Field label="App secret (optional)"><Secret value={cfg.app_secret || ''} onChange={e => setC('app_secret', e.target.value)} placeholder={s.cfg.app_secret_set ? '•••••••• saved' : 'verifies the signature'} /></Field>
      </div>
      <ol className="ol">
        <li>developers.facebook.com → your app → <b>Webhooks</b> → Page → Subscribe with the Callback URL and Verify token above, then tick <b>leadgen</b>.</li>
        <li>Connect your Page to the app (Meta Business Suite → Integrations → Leads access, allow the app).</li>
        <li>Create a <b>Page access token</b> with <i>leads_retrieval</i> and <i>pages_manage_metadata</i> and paste it above.</li>
        <li>Test with Meta's <a className="lnk" href="https://developers.facebook.com/tools/lead-ads-testing" target="_blank" rel="noopener noreferrer">Lead Ads Testing Tool</a>. Using Zapier/Make instead? Point them at the same URL – flat JSON works too.</li>
      </ol>
    </>}

    {s.type === 'google' && <>
      <Field label="Key"><CopyInput value={s.cfg.google_key} /></Field>
      <ol className="ol">
        <li>Google Ads → your campaign → <b>Assets → Lead form</b> → open the form → <b>Lead delivery options</b> → Webhook integration.</li>
        <li>Paste the Webhook URL and the Key above, click <b>Send test data</b> – a TEST lead appears in Leads.</li>
      </ol>
    </>}

    {s.type === 'website' && <>
      <Alert><b>Use only ONE of the options below for each form.</b> Webhook + script together would send every enquiry twice (Setu merges duplicates by phone/email, but you'd see double activity).</Alert>
      <details className="opt" open>
        <summary><Hi n="zap" c="violet" />Option 1 – Elementor / WordPress form plugin <span className="rec">Recommended</span></summary>
        <ol className="ol">
          <li>Edit the page in Elementor → click your <b>Form</b> widget → <b>Actions After Submit</b> → add <b>Webhook</b>.</li>
          <li>Open the new <b>Webhook</b> section → paste the <b>Endpoint URL</b> above into <i>Webhook URL</i>. "Advanced Data" can be on or off – both work.</li>
          <li>Update the page, submit the form once, then come back here: the fields appear in <b>Field mapping</b> below.</li>
        </ol>
        <p className="hint">Contact Form 7, WPForms, Gravity Forms, Fluent Forms: use their webhook add-on with the same URL. Don't add the script (Option 2) for these forms.</p>
      </details>
      <details className="opt">
        <summary><Hi n="globe" c="blue" />Option 2 – any other form on your site (script)</summary>
        <p className="sm mt">For forms without a webhook setting. Paste once before <code>&lt;/body&gt;</code> (or in your theme's footer scripts). It copies every form submission on that page to Setu and leaves your form working as before.</p>
        <pre>{s.script}</pre>
        <Btn sm ic="copy" onClick={() => copy(s.script)}>Copy script</Btn>
      </details>
      <details className="opt">
        <summary><Hi n="edit" c="teal" />Option 3 – no form yet? Paste a ready-made one</summary>
        <pre>{s.html}</pre>
        <Btn sm ic="copy" onClick={() => copy(s.html)}>Copy HTML</Btn>
        <div className="fg mt">
          <Field label="Thank-you page URL (optional)"><input type="url" value={c('thanks_url')} onChange={e => setC('thanks_url', e.target.value)} placeholder="https://yoursite.com/thank-you" /></Field>
          <Field label="Thank-you title"><input value={c('thanks_title')} onChange={e => setC('thanks_title', e.target.value)} /></Field>
          <Field label="Thank-you message"><input value={c('thanks_text')} onChange={e => setC('thanks_text', e.target.value)} placeholder="We have received your enquiry and will contact you shortly." /></Field>
        </div>
      </details>
    </>}

    {s.type === 'webhook' && <>
      <p className="sm muted">POST JSON or form data to the Endpoint URL from Zapier, Make, n8n, Pabbly or your own code.</p>
      <pre>{`curl -X POST '${s.url}' -H 'Content-Type: application/json' -d '{"name":"Ravi","phone":"+919800000000","email":"ravi@example.com","message":"Need a website"}'`}</pre>
    </>}

    {s.type !== 'google' ? <>
      <div className="row between" style={{ margin: '18px 0 8px' }}>
        <h4 className="row mt0" style={{ margin: 0 }}><Icon n="layers" />Field mapping</h4>
        <Btn sm ic="refresh" busy={busy === 'refresh'} onClick={() => run('refresh', async () => null)}>Refresh – load latest submission</Btn>
      </div>
      {!s.keys.length ? <p className="hint mt0">Waiting for the first enquiry. Submit your form once (or click <b>Send test lead</b>), then click <b>Refresh</b> – every field it sends will be listed here so you can choose where it goes.</p> :
        <div className="tw"><table className="tbl map-t"><thead><tr><th>Field received</th><th>Example value</th><th>Save into</th></tr></thead>
          <tbody>{s.keys.map(k => <tr key={k.key}>
            <td><b>{k.label}</b><br /><code className="xs">{k.key}</code></td>
            <td className="sm muted">{k.sample || '—'}</td>
            <td><select value={map[k.key] || ''} onChange={e => setMap(m => ({ ...m, [k.key]: e.target.value }))}>
              <option value="">Automatic → {k.auto_label}</option>
              {data.options.map(o => <option key={o.v} value={o.v}>{o.l}</option>)}
            </select></td>
          </tr>)}</tbody></table></div>}
    </> : <p className="hint">Google Ads sends standard columns (name, email, phone, company, city, address) which are mapped automatically; other questions go to the enquiry notes.</p>}

    {s.last_payload && <details className="mt"><summary className="sm muted" style={{ cursor: 'pointer' }}>Raw data last received</summary><pre>{s.last_payload}</pre></details>}

    <div className="card-f">
      <Btn v="n" ic="check" type="submit" busy={busy === 'save'}>Save</Btn>
      <Btn ic="send" busy={busy === 'test'} onClick={test}>Send test lead</Btn>
      <Link className="btn btn-sm" to={'/leads?src=' + s.type}><Icon n="users" />Leads</Link>
      <span style={{ marginLeft: 'auto' }}><Btn v="r" ic="trash" busy={busy === 'del'} onClick={del}>Delete</Btn></span>
    </div>
  </form>
}
