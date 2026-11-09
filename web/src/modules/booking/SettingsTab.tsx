import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { FieldDef, SettingsForm } from '@/components/SettingsForm'
import { Alert, Badge, Btn, Hi, Sec, act, confirmDo, toast } from '@/components/ui'
import { copy } from './api'
import { BrandName, useBrand } from '@/lib/theme'

const yn = { '1': 'Yes', '0': 'No' }

// v1 settings.php $T['appointments']
const APPT: FieldDef[] = [
  ['appt_title', 'Meeting title'],
  ['appt_slot', 'Slot length (minutes)', 'number'],
  ['appt_start', 'Day starts (HH:MM)'],
  ['appt_end', 'Day ends (HH:MM)'],
  ['appt_days', 'Working days', 'days'],
  ['appt_location', 'Location / meeting link', 'text', null, 'Office address or a fixed Zoom/Meet link. Leave empty when Google Calendar creates a Meet link for each booking.'],
  ['appt_notify', 'Email me when someone books', 'select', yn],
  ['appt_remind', 'Send a reminder 1 hour before', 'select', yn],
]

const GCAL: FieldDef[] = [
  ['gcal_client_id', 'Google OAuth Client ID', 'text', null, 'Optional – see "Connect Google Calendar" below.'],
  ['gcal_client_secret', 'Google OAuth Client secret', 'secret'],
  ['gcal_calendar', 'Calendar ID', 'text', null, '"primary" = your main calendar, or a calendar ID from Google Calendar → Settings → Integrate calendar.'],
  ['gcal_meet', 'Create a Google Meet link for each booking', 'select', yn],
  ['gcal_invite', 'Send Google Calendar invites to the lead', 'select', yn],
]

const DEF = { appt_title: 'Meeting', appt_slot: '30', appt_start: '10:00', appt_end: '18:00', appt_days: '1,2,3,4,5,6', appt_notify: '0', appt_remind: '0', gcal_calendar: 'primary', gcal_meet: '1', gcal_invite: '1' }

interface GStatus { ready: boolean; account: string; has_client: boolean; redirect_uri: string }

export default function AppointmentsSettings() {
  const qc = useQueryClient()
  const [sp, setSp] = useSearchParams()
  const st = useQuery({ queryKey: ['gcal-status'], queryFn: () => api.get<GStatus>('/gcal/status') })
  const [msg, setMsg] = useState<{ t: 'ok' | 'err'; m: string } | null>(null)
  useEffect(() => {
    // Result of the Google OAuth redirect (/webhooks/gcal/callback)
    if (sp.get('gcal') === 'connected') setMsg({ t: 'ok', m: 'Google Calendar connected.' })
    else if (sp.get('gcal_error')) setMsg({ t: 'err', m: sp.get('gcal_error')! })
    if (sp.get('gcal') || sp.get('gcal_error')) { sp.delete('gcal'); sp.delete('gcal_error'); setSp(sp, { replace: true }) }
  }, [])
  const reload = () => { qc.invalidateQueries({ queryKey: ['gcal-status'] }); qc.invalidateQueries({ queryKey: ['appt-summary'] }) }
  const flush = () => { api.post('/gcal/flush').catch(() => {}); reload() }
  const connect = async () => {
    const r = await act(() => api.get<{ url: string }>('/gcal/connect'))
    if (r) location.href = r.url
  }
  const disconnect = async () => {
    if (!confirmDo('Disconnect Google Calendar?')) return
    const r = await act(() => api.post<{ message: string }>('/gcal/disconnect'))
    if (r) { toast(r.message); reload() }
  }
  const g = st.data
  return <>
    {msg && <Alert t={msg.t}>{msg.m}{msg.t === 'ok' && g?.account ? ' (' + g.account + ')' : ''}</Alert>}
    <SettingsForm fields={APPT} defaults={DEF} onSaved={flush} />
    <div className="sec mt"><Hi n="cal" c="green" /><h3>Google Calendar sync <small>– optional</small></h3>{g?.ready && <span style={{ marginLeft: 'auto' }}><Badge c="green" dot>Connected{g.account ? ' · ' + g.account : ''}</Badge></span>}</div>
    <SettingsForm fields={GCAL} defaults={DEF} onSaved={flush} />
    <Sec ic="cal" c="green">Connect Google Calendar</Sec>
    <div className="row" style={{ marginBottom: 10 }}>
      {g?.ready ? <Btn sm v="r" onClick={disconnect}>Disconnect</Btn> : g?.has_client ? <Btn v="p" sm ic="link" onClick={connect}>Connect Google Calendar</Btn> : <span className="muted sm">Save your Client ID and Client secret above, then connect.</span>}
    </div>
    <p className="sm muted mt0"><b>Best of both:</b> bookings are managed here in <BrandName /> (pipeline, reminders, automations), and – when connected – each booking is added to your Google Calendar with a Meet link, and your busy times in Google block those slots on the booking page.</p>
    <ol className="ol">
      <li>console.cloud.google.com → APIs &amp; Services → <b>Enable "Google Calendar API"</b>.</li>
      <li><b>OAuth consent screen</b> → External → add your email as a test user (or publish the app).</li>
      <li><b>Credentials → Create credentials → OAuth client ID</b> → Web application → Authorised redirect URI:
        {g && <span className="copy"><input readOnly value={g.redirect_uri} /><Btn sm ic="copy" title="Copy" onClick={() => { copy(g.redirect_uri); toast('Copied.') }} /></span>}</li>
      <li>Paste the Client ID and Client secret above, Save, then click <b>Connect Google Calendar</b>.</li>
    </ol>
  </>
}
