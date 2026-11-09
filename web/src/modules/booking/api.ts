// Types and small helpers of the booking module.
export type ApptStatus = 'scheduled' | 'completed' | 'no_show' | 'cancelled'

export interface Appt {
  id: number
  lead_id: number
  user_id: number | null
  title: string
  start_at: string
  end_at: string
  status: ApptStatus
  location: string
  notes: string
  booked_by: 'lead' | 'admin'
  reminded: boolean
  meet_link: string
  in_gcal: boolean
  name: string
  contact_name: string
  phone: string
  email: string
  user_name: string
  day: string // YYYY-MM-DD (workspace time zone)
  time: string // 03:04 PM
  local: string // YYYY-MM-DDTHH:MM
  mins: number
}

export interface Summary {
  kpi: { today: number; upcoming: number; completed: number; no_show: number; self_booked: number }
  upcoming: { label: string; rows: Appt[] }[]
  link: string
  setup: { start: string; end: string; slot: number; remind: boolean; notify: boolean; gcal: boolean; title: string; location: string }
  today: string
  now: string
  tz: string
}

export interface ApptPage { rows: Appt[]; total: number; page: number; pages: number }

export const STATUS: Record<ApptStatus, [string, string]> = {
  scheduled: ['Scheduled', 'blue'],
  completed: ['Held', 'green'],
  no_show: ['No-show', 'orange'],
  cancelled: ['Cancelled', 'gray'],
}

export const who = (a: Pick<Appt, 'contact_name' | 'name'>) => a.contact_name || a.name || '—'

/** Add days to a YYYY-MM-DD string (calendar maths without time zones). */
export function addDays(d: string, n: number) {
  const [y, m, dd] = d.split('-').map(Number)
  const t = new Date(Date.UTC(y, m - 1, dd + n))
  return t.toISOString().slice(0, 10)
}

export function copy(text: string) {
  navigator.clipboard?.writeText(text)
}
