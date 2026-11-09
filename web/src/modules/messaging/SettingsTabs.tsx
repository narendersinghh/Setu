// Settings → Messaging: Email, WhatsApp, AI voice calls (fields as in v1 settings.php).
import { FormEvent, useState } from 'react'
import { api } from '@/lib/api'
import { useMe } from '@/lib/session'
import { Btn, Card, Spinner, toast } from '@/components/ui'
import { SettingsForm, FieldDef, useSettings } from '@/components/SettingsForm'
import { useQueryClient } from '@tanstack/react-query'
import { useMessagingMeta } from './LeadChannels'
import { BrandName, useBrand } from '@/lib/theme'

const o = (a: string[]) => Object.fromEntries(a.map(x => [x, x]))

async function run(url: string, body: object = {}) {
  try {
    const r = await api.post<{ message?: string; link?: string }>(url, body)
    if (r.link) window.open(r.link, '_blank', 'noopener')
    toast(r.message || 'Done.')
  } catch (e: any) { toast(e.message) }
}

export function EmailTab() {
  const brand = useBrand()
  const meta = useMessagingMeta()
  const me = useMe()
  const s = useSettings()
  const qc = useQueryClient()
  const [to, setTo] = useState(me.user.email)
  const [busy, setBusy] = useState('')
  if (!meta.data) return <Spinner />
  const apis = Object.fromEntries(meta.data.email_apis.map(a => [a.key, a.name]))
  const presets = Object.fromEntries(meta.data.smtp_presets.map(p => [p.key, p.name]))
  const f: FieldDef[] = [
    ['mail_transport', 'How should ' + brand.name + ' send email?', 'select', { default: 'Default mailer – works without any set-up', smtp: 'My own mailbox (SMTP) – Gmail, Zoho, Outlook, Hostinger, cPanel…', api: 'Email service API – Brevo, SendGrid, Mailjet…' }],
    ['mail_api', 'Email service', 'select', { '': 'Choose…', ...apis }, '', 'mail_transport=api'],
    ['mail_api_key', 'API key', 'secret', null, 'The key of the service chosen above – see "Which option?" below for where to get it, then verify your From email or domain in that account.', 'mail_transport=api'],
    ['mail_api_secret', 'Secret key (Mailjet shows it next to the API key)', 'secret', null, '', 'mail_transport=api;mail_api=mailjet'],
    ['mail_api_domain', 'Sending domain (as added in Mailgun)', 'text', null, 'e.g. mg.yourdomain.com', 'mail_transport=api;mail_api=mailgun'],
    ['mail_api_region', 'Account region', 'select', { '': 'Default (US for Mailgun, India for ZeptoMail)', com: 'Global / US (ZeptoMail .com)', eu: 'Europe (EU)' }, '', 'mail_transport=api;mail_api=mailgun,zeptomail'],
    ['smtp_provider', 'Mailbox provider', 'select', presets, '', 'mail_transport=smtp'],
    ['from_email', 'From email', 'email', null, 'Use an address on your own domain (e.g. hello@yourdomain.com). With an email service, this address or its domain must be verified there; with a mailbox, it is also your login.'],
    ['from_name', 'From name'],
    ['smtp_pass', 'Mailbox password', 'secret', null, 'Gmail: App password (Google Account → Security → App passwords) · Zoho: mailbox or app-specific password · Outlook: password or app password · SES: SMTP password from SES → SMTP settings', 'mail_transport=smtp'],
    ['smtp_user', 'Login (only if different from the From email)', 'text', null, '', 'mail_transport=smtp;smtp_provider=custom,ses'],
    ['smtp_host', 'SMTP server', 'text', null, 'e.g. mail.yourdomain.com', 'mail_transport=smtp;smtp_provider=custom,ses,cpanel'],
    ['smtp_port', 'SMTP port', 'text', null, '', 'mail_transport=smtp;smtp_provider=custom,ses,cpanel'],
    ['smtp_secure', 'Encryption', 'select', { ssl: 'SSL (465)', tls: 'TLS / STARTTLS (587)', none: 'None' }, '', 'mail_transport=smtp;smtp_provider=custom,ses,cpanel'],
    ['reply_to', 'Reply-to email (optional)', 'email'],
    ['bcc_email', 'BCC a copy to (optional)', 'email'],
    ['imap_on', 'Detect replies by reading your inbox (IMAP)', 'select', { '0': 'Off', '1': 'On – mark leads as replied automatically' }, <><BrandName /> checks the inbox every 2 minutes.</>],
    ['imap_host', 'IMAP server', 'text', null, 'e.g. imap.hostinger.com, imap.gmail.com', 'imap_on=1'],
    ['imap_port', 'IMAP port', 'text', null, '993 = SSL; other ports use STARTTLS when the server offers it.', 'imap_on=1'],
    ['imap_user', 'IMAP login', 'text', null, 'Leave empty to use the mailbox login.', 'imap_on=1'],
    ['imap_pass', 'IMAP password', 'secret', null, 'Leave empty to use the mailbox password.', 'imap_on=1'],
  ]
  const test = async (k: string, url: string, body: object = {}) => { setBusy(k); await run(url, body); setBusy('') }
  const imap = s.data?.imap_host
  return <>
    <SettingsForm fields={f} defaults={{ mail_transport: 'default', smtp_provider: 'custom', smtp_port: '465', smtp_secure: 'ssl', imap_on: '0', imap_port: '993' }}
      onSaved={() => qc.invalidateQueries({ queryKey: ['meta-messaging'] })}
      extra={<>
        <span className="row nw" style={{ gap: 6, marginLeft: 'auto' }}>
          <input type="email" value={to} onChange={e => setTo(e.target.value)} placeholder="Send test to…" style={{ margin: 0, width: 230 }} />
          <Btn ic="mail" busy={busy === 'e'} onClick={() => test('e', '/messaging/test/email', { to })}>Send test email</Btn>
        </span>
        {imap && <Btn ic="refresh" busy={busy === 'i'} onClick={() => test('i', '/messaging/test/imap')}>Test IMAP</Btn>}
      </>} />
    <Card title="Which option?" ic="book" c="blue">
      <ul className="ul mt0">
        <li><b>Email service (recommended)</b> – create a free account at Brevo, SendGrid, Mailjet, Postmark, Resend, ZeptoMail, Mailgun, SMTP2GO or Elastic Email, verify your domain or From email there, copy its <b>API key</b> and paste it here. Nothing else to set up.</li>
        <li><b>My mailbox</b> – sends from your Gmail, Zoho, Outlook, Hostinger or cPanel mailbox: enter the From email and its password (Gmail/Zoho need an app password).</li>
        <li><b>Default mailer</b> – emails go out right away with your company name, and replies come to your company email. For the best delivery and to send from your own address, connect your mailbox (SMTP) or an email service.</li>
      </ul>
      <p className="sm muted">Where to get the API key:</p>
      <ul className="ul sm">{meta.data.email_apis.map(a => <li key={a.key}><b>{a.name}</b> – {a.paste}: <a className="lnk" target="_blank" rel="noopener noreferrer" href={a.url}>Get your key ↗</a></li>)}</ul>
    </Card>
  </>
}

export function WhatsAppTab() {
  const meta = useMessagingMeta()
  const [to, setTo] = useState('')
  const [busy, setBusy] = useState(false)
  const hook = meta.data?.webhooks.whatsapp || '/webhooks/whatsapp'
  const f: FieldDef[] = [
    ['wa_provider', 'How to send WhatsApp messages', 'select', { manual: 'Manual – you tap send (free, no API)', cloud: 'WhatsApp Cloud API (Meta, official, automatic)', generic: 'Other provider API (Interakt, WATI, AiSensy…)' }],
    ['wa_phone_id', 'Phone number ID', 'text', null, '', 'wa_provider=cloud'],
    ['wa_access_token', 'Permanent access token', 'secret', null, '', 'wa_provider=cloud'],
    ['wa_api_version', 'Graph API version', 'text', null, '', 'wa_provider=cloud'],
    ['wa_template', 'Approved template name', 'text', null, 'Business-initiated messages need an approved template with a DOCUMENT header (the PDF is attached to it). Leave empty to send the text as the PDF caption (works within 24h of the customer messaging you).', 'wa_provider=cloud'],
    ['wa_template_lang', 'Template language code', 'text', null, '', 'wa_provider=cloud'],
    ['wa_template_params', 'Template body variables (separated by |)', 'text', null, 'Example: {{business_name}}|{{sender_name}} → fills {{1}}, {{2}}.', 'wa_provider=cloud'],
    ['wa_app_secret', 'App secret (optional, verifies webhook signatures)', 'secret', null, '', 'wa_provider=cloud'],
    ['wa_verify_token', 'Webhook verify token', 'secret', null, <>Choose any word and enter the same in Meta. Webhook URL: <code>{hook}</code> – subscribe to "messages" for delivered/read/replies.</>, 'wa_provider=cloud'],
    ['wa_api_url', 'Endpoint URL', 'url', null, 'Placeholders: {{phone}}, {{token}}', 'wa_provider=generic'],
    ['wa_api_method', 'Method', 'select', { POST: 'POST', GET: 'GET' }, '', 'wa_provider=generic'],
    ['wa_api_token', 'Token', 'secret', null, '', 'wa_provider=generic'],
    ['wa_api_headers', 'Headers (JSON)', 'textarea', null, '', 'wa_provider=generic'],
    ['wa_api_body', 'Body template (JSON)', 'textarea', null, 'Sent once per PDF. Placeholders: {{phone}} {{message}} {{file_url}} {{file_name}} {{file_base64}} {{token}}.', 'wa_provider=generic'],
  ]
  const saved = async () => {
    try {
      const r = await api.post<{ ok: boolean; error?: string }>('/messaging/whatsapp/register')
      if (!r.ok && r.error) toast(r.error)
    } catch (e: any) { toast(e.message) }
  }
  const test = async (e: FormEvent) => { e.preventDefault(); setBusy(true); await run('/messaging/test/whatsapp', { to }); setBusy(false) }
  return <>
    <SettingsForm fields={f} defaults={{ wa_provider: 'manual', wa_api_version: 'v21.0', wa_template_lang: 'en', wa_template_params: '{{business_name}}', wa_api_method: 'POST' }} onSaved={saved} />
    <Card title="Test & how PDFs are delivered" ic="wa" c="green">
      <form className="row nw" onSubmit={test}><input value={to} onChange={e => setTo(e.target.value)} placeholder="Your WhatsApp number" required style={{ margin: 0 }} /><Btn type="submit" ic="send" busy={busy}>Send test</Btn></form>
      <ul className="ul">
        <li><b>WhatsApp Cloud API</b> – the PDF is uploaded and arrives as a real file attachment with your message as caption.</li>
        <li><b>Other provider</b> – most APIs need a public file link ({'{{file_url}}'}); if yours accepts the file itself, use {'{{file_base64}}'}.</li>
        <li><b>Manual</b> – click <i>WhatsApp + PDF</i> on a lead. On a phone the PDF is shared straight into the chat; on a computer it downloads and WhatsApp Web opens with the message ready.</li>
      </ul>
    </Card>
  </>
}

export function VoiceTab() {
  const meta = useMessagingMeta()
  const qc = useQueryClient()
  const [to, setTo] = useState('')
  const [busy, setBusy] = useState(false)
  if (!meta.data) return <Spinner />
  const m = meta.data
  const f: FieldDef[] = [['voice_provider', 'Voice AI provider', 'select', { '': '— Not connected —', ...Object.fromEntries(m.voice_providers.map(p => [p.key, p.name])) },
    <>{m.voice_providers.map(p => <span key={p.key} className="block"><b>{p.name}</b> – {p.desc}<br /></span>)}</>]]
  m.voice_providers.forEach(p => p.fields.forEach(x => f.push([x.key, p.name + ': ' + x.label, x.type, null, x.key === 'voice_body' ? <>The call result comes back to <code>{'{{webhook}}'}</code> – pass it to your provider as its webhook URL.</> : '', 'voice_provider=' + p.key])))
  f.push(['voice_language', 'Default language', 'select', o(m.voice_languages)],
    ['voice_first_message', 'Default first sentence'],
    ['voice_task', 'Default call script / task', 'textarea', null, 'Placeholders like {{contact_name}}, {{business_name}}, {{agency_name}} work here.'])
  const test = async (e: FormEvent) => { e.preventDefault(); setBusy(true); await run('/messaging/test/voice', { to }); setBusy(false) }
  return <>
    <SettingsForm fields={f} defaults={{ voice_language: 'English' }} onSaved={() => qc.invalidateQueries({ queryKey: ['meta-messaging'] })} />
    <Card title="Test & Sarvam AI setup" ic="mic" c="pink">
      <form className="row nw" onSubmit={test}><input value={to} onChange={e => setTo(e.target.value)} placeholder="Your phone number (+91…)" required style={{ margin: 0 }} /><Btn type="submit" ic="phone" busy={busy} disabled={!m.voice_ready}>Call me</Btn></form>
      <ol className="ol">
        <li>Sign in at sarvam.ai → <b>Samvaad / Voice agents</b> and build your agent (language, voice, script). Use variables like <code>{'{{business_name}}'}</code>, <code>{'{{contact_name}}'}</code>, <code>{'{{task}}'}</code> – <BrandName /> sends them.</li>
        <li>Connect a phone number (telephony connection) to the agent.</li>
        <li>Copy the Org ID, Workspace ID, Agent (app) ID, version, Connection ID and agent phone number into the fields above, plus a workspace API key.</li>
        <li>Call results (status, transcript, summary) come back automatically to each lead's journey. Retell posts to one webhook for all calls: set it to <code>{m.webhooks.call}</code>.</li>
      </ol>
    </Card>
  </>
}
