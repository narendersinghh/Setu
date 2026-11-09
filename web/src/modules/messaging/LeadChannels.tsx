// Reusable pieces for the lead page (owned by crm): AI calls card, call modal, email & WhatsApp modals.
import { FormEvent, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useCan } from '@/lib/session'
import { ago } from '@/lib/format'
import { Alert, Badge, Btn, Card, Field, Icon, Modal, toast } from '@/components/ui'

export interface MessagingMeta {
  email_apis: { key: string; name: string; paste: string; url: string; extra: string }[]
  smtp_presets: { key: string; name: string; host: string; port: string; secure: string; hint: string }[]
  voice_providers: { key: string; name: string; desc: string; fields: { key: string; label: string; type: 'text' | 'secret' | 'textarea' }[] }[]
  voice_languages: string[]
  webhooks: { whatsapp: string; call: string }
  mail_ready: boolean; mail_label: string; imap_on: boolean; wa_provider: string; voice_ready: boolean
  voice_provider: string; voice_first_message: string; voice_task: string
}

export function useMessagingMeta() {
  return useQuery({ queryKey: ['meta-messaging'], queryFn: () => api.get<MessagingMeta>('/meta/messaging'), staleTime: 30_000 })
}

const CH: Record<string, string> = { sent: 'teal', delivered: 'teal', read: 'green', opened: 'green', failed: 'red', manual: 'gold', queued: 'blue', received: 'blue' }
/** Delivery status badge (v1 chbadge). */
export function chBadge(s: string) {
  return s ? <Badge c={CH[s] || 'gray'}>{s.charAt(0).toUpperCase() + s.slice(1)}</Badge> : <span className="muted">—</span>
}

interface CallRow { id: number; provider: string; status: string; duration: number | null; summary: string; transcript: string; recording: string; created_at: string }

/** AI calls card of a lead. */
export function CallsCard({ leadId, phone }: { leadId: number; phone: string }) {
  const meta = useMessagingMeta()
  const calls = useQuery({ queryKey: ['calls', leadId], queryFn: () => api.get<CallRow[]>('/leads/' + leadId + '/calls') })
  const [open, setOpen] = useState(false)
  const mmss = (n: number) => String(Math.floor(n / 60)).padStart(2, '0') + ':' + String(n % 60).padStart(2, '0')
  return <Card title="AI calls" ic="mic" c="pink" actions={<Btn sm v="p" ic="phone" disabled={!phone} onClick={() => setOpen(true)}>Call now</Btn>}>
    {!calls.data?.length && <div className="empty sm"><Icon n="mic" /><p>{meta.data?.voice_ready ? 'No calls yet.' : 'Connect an AI voice provider in Settings → Messaging → AI voice calls.'}</p></div>}
    {calls.data?.map(c => <div className="msg" key={c.id}>
      <div className="msg-h"><Icon n="mic" /><b>{c.provider.charAt(0).toUpperCase() + c.provider.slice(1)}</b><Badge c={['completed', 'ended', 'done'].includes(c.status) ? 'green' : c.status === 'failed' ? 'red' : 'blue'}>{c.status}</Badge><small>{ago(c.created_at)}{c.duration ? ' · ' + mmss(c.duration) : ''}</small></div>
      {c.summary && <p>{c.summary}</p>}
      {c.transcript && <details><summary className="sm">Transcript</summary><p className="sm" style={{ whiteSpace: 'pre-wrap' }}>{c.transcript}</p></details>}
      {c.recording && /^https?:\/\//.test(c.recording) && <audio controls src={c.recording} style={{ width: '100%' }} />}
    </div>)}
    <CallModal open={open} onClose={() => setOpen(false)} leadId={leadId} phone={phone} />
  </Card>
}

export function CallModal({ open, onClose, leadId, phone }: { open: boolean; onClose: () => void; leadId: number; phone: string }) {
  const meta = useMessagingMeta()
  const can = useCan()
  const qc = useQueryClient()
  const [first, setFirst] = useState('')
  const [task, setTask] = useState('')
  const [lang, setLang] = useState('')
  const [busy, setBusy] = useState(false)
  const go = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api.post<{ message: string }>('/leads/' + leadId + '/call', { task, first_message: first, language: lang })
      toast(r.message)
      qc.invalidateQueries({ queryKey: ['calls', leadId] })
      qc.invalidateQueries({ queryKey: ['journey', leadId] })
      onClose()
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const m = meta.data
  return <Modal open={open} onClose={onClose} title="AI voice call" ic="mic" c="pink">
    <form onSubmit={go}>
      {m && !m.voice_ready && <Alert t="warn">No voice provider connected.{can('settings') ? <> <Link to="/settings/voice">Connect Sarvam AI, Bland, Vapi or Retell</Link>.</> : ' Ask your admin to connect one.'}</Alert>}
      <p className="muted">The AI calls <b>{phone}</b>. Leave blank to use the default script from Settings.</p>
      <Field label="First sentence"><input value={first} onChange={e => setFirst(e.target.value)} placeholder={m?.voice_first_message} /></Field>
      <Field label="Task / script for this call"><textarea rows={4} value={task} onChange={e => setTask(e.target.value)} placeholder={m?.voice_task} /></Field>
      <Field label="Language"><select value={lang} onChange={e => setLang(e.target.value)}><option value="">Default</option>{m?.voice_languages.map(l => <option key={l}>{l}</option>)}</select></Field>
      <div className="card-f"><Btn v="p" type="submit" ic="phone" busy={busy} disabled={!m?.voice_ready}>Start call</Btn></div>
    </form>
  </Modal>
}

/** Email to a lead (subject/body with placeholders, optional report PDF). */
export function EmailModal({ open, onClose, leadId, hasReport }: { open: boolean; onClose: () => void; leadId: number; hasReport?: boolean }) {
  const qc = useQueryClient()
  const [sub, setSub] = useState('')
  const [body, setBody] = useState('')
  const [attach, setAttach] = useState(false)
  const [busy, setBusy] = useState(false)
  const go = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api.post<{ message: string }>('/leads/' + leadId + '/email', { subject: sub, body, attach_report: attach })
      toast(r.message)
      qc.invalidateQueries({ queryKey: ['journey', leadId] })
      qc.invalidateQueries({ queryKey: ['messages', leadId] })
      setSub(''); setBody(''); onClose()
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  return <Modal open={open} onClose={onClose} title="Send email" ic="mail" c="blue">
    <form onSubmit={go}>
      <Field label="Subject"><input value={sub} onChange={e => setSub(e.target.value)} required /></Field>
      <Field label="Message" hint="Placeholders: {{first_name}} {{business_name}} {{booking_link}} {{report_link}} {{sender_name}}"><textarea rows={7} value={body} onChange={e => setBody(e.target.value)} required /></Field>
      {hasReport && <label className="chk sm"><input type="checkbox" checked={attach} onChange={e => setAttach(e.target.checked)} /> Attach the audit report PDF</label>}
      <div className="card-f"><Btn v="p" type="submit" ic="send" busy={busy}>Send</Btn></div>
    </form>
  </Modal>
}

/** Send a WhatsApp message; in manual mode opens WhatsApp with the text ready. */
export async function sendWhatsApp(leadId: number, message: string, attachReport: boolean) {
  const r = await api.post<{ ok: boolean; link?: string; message?: string }>('/leads/' + leadId + '/whatsapp', { message, attach_report: attachReport })
  if (r.link) window.open(r.link, '_blank', 'noopener')
  else if (r.message) toast(r.message)
  return r
}
