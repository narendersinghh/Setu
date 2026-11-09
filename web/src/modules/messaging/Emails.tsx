// Emails: every email to and from leads in one place – conversations per lead, replies pulled from the
// inbox, answer from here (v1 app/pages/emails.php).
import { FormEvent, useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useCan } from '@/lib/session'
import { Alert, Btn, Empty, Field, Icon, IconBtn, Modal, Spinner, toast, usePageTitle, useInterval } from '@/components/ui'
import { chBadge } from './LeadChannels'

interface Conv { id: number; name: string; contact_name: string; email: string; at: string; un: number; ins: number; n: number; last_dir: string; last_subject: string; last_body: string }
interface ListRes { conversations: Conv[]; total: number; unread: number; ready: boolean; imap: boolean; imap_last: number; imap_error: string; can_settings: boolean }
interface Msg { id: number; direction: 'in' | 'out'; status: string; subject: string; body: string; created_at: string }
interface ThreadRes { lead: { id: number; name: string; contact_name: string; email: string }; thread: Msg[]; reply_id: number; subject: string; has_report: boolean; ready: boolean; imap: boolean }

const COLORS = ['indigo', 'violet', 'blue', 'teal', 'green', 'amber', 'orange', 'pink']

function when(ts: string) {
  const d = new Date(ts), now = new Date()
  return d.toDateString() === now.toDateString() ? d.toLocaleTimeString('en-IN', { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString('en-GB', { day: '2-digit', month: 'short' })
}

export default function Emails() {
  usePageTitle('Emails', 'Every email with your leads – read replies and answer without leaving Setu')
  const [sp, setSp] = useSearchParams()
  const lead = Number(sp.get('lead') || 0), f = sp.get('f') || '', q = sp.get('q') || ''
  const [search, setSearch] = useState(q)
  const [newOpen, setNewOpen] = useState(false)
  const qc = useQueryClient()
  const can = useCan()
  const list = useQuery({ queryKey: ['emails', f, q], queryFn: () => api.get<ListRes>('/emails', { f, q }) })
  const go = (p: Record<string, string | number>) => {
    const n = new URLSearchParams()
    const all = { f, q, lead: lead || '', ...p }
    Object.entries(all).forEach(([k, v]) => { if (v !== '' && v !== 0) n.set(k, String(v)) })
    setSp(n)
  }
  const refresh = () => { qc.invalidateQueries({ queryKey: ['emails'] }); qc.invalidateQueries({ queryKey: ['email-thread'] }); qc.invalidateQueries({ queryKey: ['nav'] }) }
  const sync = async (manual: boolean) => {
    try {
      const r = await api.post<{ n: number; error?: string }>('/emails/sync')
      if (manual) toast(r.error ? r.error : r.n ? r.n + ' new email(s) received.' : 'No new emails.')
      if (r.n > 0) refresh()
    } catch (e: any) { if (manual) toast(e.message) }
  }
  useInterval(() => sync(false), list.data?.imap ? 60000 : null)
  const d = list.data
  const tabs: [string, string, number | undefined][] = [['', 'All', d?.total], ['unread', 'Unread', d?.unread], ['replied', 'Replied', undefined], ['waiting', 'Awaiting reply', undefined]]
  const setLink = can('settings') ? <Link className="lnk" to="/settings/email">Settings → Email</Link> : <>Settings → Email</>
  return <>
    {d && !d.ready && <Alert t="warn"><b>Email is not set up yet.</b> {d.can_settings ? <><Link className="lnk" to="/settings/email">Connect your email service or mailbox</Link> to send from here.</> : 'Ask your admin to connect email in Settings.'}</Alert>}
    {d && d.ready && !d.imap && <Alert t="info"><b>Replies are not coming in yet.</b> Turn on <i>Detect replies by reading your inbox (IMAP)</i> in {setLink} with the mailbox your leads reply to (your From or Reply-to address). Setu then brings every reply here automatically.</Alert>}
    {d && d.imap && d.imap_error && <Alert t="err">Inbox check failed: {d.imap_error}</Alert>}
    <div className={'ib-w' + (lead ? ' has-t' : '')}>
      <div className="card p0 ib-l">
        <div className="ib-lh">
          <form className="ib-s" onSubmit={e => { e.preventDefault(); go({ q: search.trim(), lead: '' }) }}><input value={search} onChange={e => setSearch(e.target.value)} placeholder="Search name, email, subject" /></form>
          <Btn v="p" sm ic="plus" title="New email" onClick={() => setNewOpen(true)}>New</Btn>
        </div>
        <div className="ib-f">{tabs.map(([k, lb, n]) => <a key={k} href="#" className={f === k ? 'on' : ''} onClick={e => { e.preventDefault(); go({ f: k }) }}>{lb}{n ? <em>{n}</em> : null}</a>)}</div>
        <div className="ib-c">
          {!d ? <Spinner /> : !d.conversations.length && <div className="empty sm"><Icon n="mail" /><p>{q || f ? 'No conversations match.' : 'No emails yet. Emails sent to leads – reports, automations and your own – appear here.'}</p></div>}
          {d?.conversations.map(c => <a key={c.id} href="#" className={'ib-i' + (lead === c.id ? ' on' : '') + (c.un ? ' un' : '')} onClick={e => { e.preventDefault(); go({ lead: c.id }) }}>
            <span className={'avc c-' + COLORS[c.id % 8]}>{(c.contact_name || c.name || '?').charAt(0).toUpperCase()}</span>
            <span className="grow">
              <span className="ib-n"><b>{c.contact_name || c.name}</b><small>{when(c.at)}</small></span>
              <span className="ib-sub">{c.last_dir === 'out' ? 'You: ' : ''}{c.last_subject}</span>
              <span className="ib-p">{c.last_body}</span>
            </span>
            {c.un > 0 && <span className="ib-b">{c.un}</span>}
          </a>)}
        </div>
      </div>
      <div className="card p0 ib-t">
        {!lead ? <div className="empty"><Icon n="mail" c="eic" /><h4>Select a conversation</h4><p>Pick a lead on the left, or start a <a className="lnk" href="#" onClick={e => { e.preventDefault(); setNewOpen(true) }}>new email</a>.</p>
          {d?.imap && <Btn sm ic="refresh" className="mt" onClick={() => sync(true)}>Check for new emails</Btn>}</div>
          : <Thread id={lead} onBack={() => go({ lead: '' })} onSync={() => sync(true)} onSent={refresh} />}
      </div>
    </div>
    <NewEmail open={newOpen} ready={!!d?.ready} onClose={() => setNewOpen(false)} onSent={id => { setNewOpen(false); refresh(); go({ lead: id }) }} />
  </>
}

function Thread({ id, onBack, onSync, onSent }: { id: number; onBack: () => void; onSync: () => void; onSent: () => void }) {
  const qc = useQueryClient()
  const t = useQuery({ queryKey: ['email-thread', id], queryFn: () => api.get<ThreadRes>('/emails/' + id) })
  const [sub, setSub] = useState('')
  const [body, setBody] = useState('')
  const [attach, setAttach] = useState(false)
  const [busy, setBusy] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => { if (t.data) { setSub(t.data.subject); qc.invalidateQueries({ queryKey: ['emails'] }); qc.invalidateQueries({ queryKey: ['nav'] }) } }, [t.data?.lead.id, t.data?.thread.length])
  useEffect(() => { if (box.current) box.current.scrollTop = box.current.scrollHeight }, [t.data?.thread.length])
  if (t.isError) return <div className="empty"><p>{(t.error as Error).message}</p></div>
  if (!t.data) return <Spinner />
  const { lead, thread } = t.data
  const send = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api.post<{ message: string }>('/leads/' + lead.id + '/email', { subject: sub, body, attach_report: attach, reply: t.data!.reply_id })
      toast(r.message)
      setBody('')
      setAttach(false)
      onSent()
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  const name = lead.contact_name || lead.name
  return <>
    <div className="ib-th">
      <a className="ib-bk" href="#" title="Back" onClick={e => { e.preventDefault(); onBack() }}>‹</a>
      <div className="grow"><b>{name}</b><small>{lead.email}{lead.contact_name ? ' · ' + lead.name : ''}</small></div>
      {t.data.imap && <IconBtn bd ic="refresh" title="Check for new emails" onClick={onSync} />}
      <Link className="btn btn-sm" to={'/leads/' + lead.id}>Open lead</Link>
    </div>
    <div className="ib-ms" ref={box}>
      {!thread.length && <div className="empty sm"><p>No emails with this lead yet – write the first one below.</p></div>}
      {thread.map(m => <div key={m.id} className={'em ' + (m.direction === 'in' ? 'em-in' : 'em-out')}>
        <div className="em-h"><b>{m.direction === 'in' ? name : 'You'}</b><span className="muted">{new Date(m.created_at).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', hour12: true })}</span>{m.direction === 'out' && chBadge(m.status)}</div>
        <div className="em-s">{m.subject}</div>
        <div className="em-b" style={{ whiteSpace: 'pre-wrap' }}>{m.body.trim()}</div>
      </div>)}
    </div>
    <form className="ib-r" onSubmit={send}>
      <input value={sub} onChange={e => setSub(e.target.value)} placeholder="Subject" required />
      <textarea rows={4} value={body} onChange={e => setBody(e.target.value)} placeholder="Write your reply… ({{first_name}}, {{booking_link}}, {{report_link}} work here)" required />
      <div className="ib-ra">
        {t.data.has_report && <label className="chk sm"><input type="checkbox" checked={attach} onChange={e => setAttach(e.target.checked)} /> Attach the audit report PDF</label>}
        <Btn v="p" type="submit" ic="send" busy={busy} disabled={!t.data.ready || !lead.email}>Send</Btn>
      </div>
    </form>
  </>
}

function NewEmail({ open, ready, onClose, onSent }: { open: boolean; ready: boolean; onClose: () => void; onSent: (lead: number) => void }) {
  const [q, setQ] = useState('')
  const leads = useQuery({ queryKey: ['email-leads', q], queryFn: () => api.get<{ id: number; name: string; contact_name: string; email: string }[]>('/emails/leads', { q }), enabled: open })
  const [lead, setLead] = useState('')
  const [sub, setSub] = useState('')
  const [body, setBody] = useState('')
  const [attach, setAttach] = useState(false)
  const [busy, setBusy] = useState(false)
  const send = async (e: FormEvent) => {
    e.preventDefault()
    if (!lead) return toast('Choose a lead.')
    setBusy(true)
    try {
      const r = await api.post<{ message: string }>('/leads/' + lead + '/email', { subject: sub, body, attach_report: attach })
      toast(r.message)
      setSub(''); setBody(''); setAttach(false)
      onSent(Number(lead))
    } catch (err: any) { toast(err.message) } finally { setBusy(false) }
  }
  return <Modal open={open} onClose={onClose} title="New email" ic="mail" c="blue">
    <form onSubmit={send}>
      <Field label="Find a lead"><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search name or email" /></Field>
      <Field label="To (lead)"><select value={lead} onChange={e => setLead(e.target.value)} required><option value="">Choose a lead…</option>{leads.data?.map(x => <option key={x.id} value={x.id}>{(x.contact_name || x.name) + ' – ' + x.email}</option>)}</select></Field>
      <Field label="Subject"><input value={sub} onChange={e => setSub(e.target.value)} required /></Field>
      <Field label="Message" hint="Placeholders: {{first_name}} {{business_name}} {{booking_link}} {{report_link}} {{sender_name}}"><textarea rows={7} value={body} onChange={e => setBody(e.target.value)} required placeholder="Hi {{first_name}}, …" /></Field>
      <label className="chk sm"><input type="checkbox" checked={attach} onChange={e => setAttach(e.target.checked)} /> Attach the audit report PDF (when the lead has one)</label>
      <div className="card-f"><Btn v="p" type="submit" ic="send" busy={busy} disabled={!ready}>Send</Btn></div>
    </form>
  </Modal>
}
