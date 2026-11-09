// Types and small helpers shared by the automation pages.
import { Icon } from '@/components/ui'

export type Step = { t: string; [k: string]: any }
export interface Cond { f: string; op: string; v: string }

export interface Automation {
  id: number
  name: string
  trig: string
  trig_value: string
  conditions: Cond[]
  steps: Step[]
  stop_on_reply: boolean
  active: boolean
  runs: number
  created_at: string
}

export interface AutoRow extends Automation { running: number; completed: number }

export interface LogEntry { at: string; step: string; msg: string; st: 'ok' | 'skip' | 'wait' | 'err' | 'end' }

export interface Run {
  id: number
  automation_id: number
  automation: string
  lead_id: number
  lead: string
  step: number
  steps: number
  status: string
  next_at: string | null
  log: LogEntry[]
  created_at: string
}

export interface Trigger { key: string; label: string; desc: string; value: string }
export interface Action { key: string; label: string; ic: string; c: string }

export interface Meta {
  triggers: Trigger[]
  actions: Action[]
  conds: [string, string][]
  ops: [string, string][]
  statuses: [string, string, string][]
  sources: [string, string][]
  users: { id: number; name: string }[]
  vars: string
  ready: { email: boolean; whatsapp: boolean; voice: boolean }
  prospecting: boolean
}

/** v1 action colours → UI kit colour names. */
export const CC: Record<string, string> = { green: 'green', blue: 'blue', purple: 'violet', orange: 'orange', gray: 'gray', teal: 'teal', red: 'rose', gold: 'amber' }

export const RUN_BADGE: Record<string, string> = { running: 'blue', waiting: 'gold', done: 'green', failed: 'red', stopped: 'gray', cancelled: 'gray' }

export const unit = (s: Step) => { const n = Number(s.n || 1); const u = s.u || 'hours'; return n + ' ' + (n === 1 ? u.replace(/s$/, '') : u) }

/** Compact flow: icon chips with arrows (v1 au-flow). */
export function Flow({ steps, actions, max = 7 }: { steps: Step[]; actions: Action[]; max?: number }) {
  const AC = Object.fromEntries(actions.map(a => [a.key, a]))
  return <div className="au-flow">
    {steps.slice(0, max).map((s, i) => { const a = AC[s.t] || { label: '?', ic: 'zap' }; return <span key={i} style={{ display: 'contents' }}>{i > 0 && <em className="arr">→</em>}<span><Icon n={a.ic} />{s.t === 'wait' ? 'Wait ' + unit(s) : a.label}</span></span> })}
    {steps.length > max && <span>+{steps.length - max} more</span>}
  </div>
}

/** Log lines of a run (stored as a list of entries). */
export function RunLog({ log, tz }: { log: LogEntry[]; tz?: string }) {
  if (!log?.length) return null
  const f = (s: string) => new Date(s).toLocaleString('en-IN', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false, timeZone: tz })
  return <p style={{ whiteSpace: 'pre-wrap' }}>{log.map((e, i) => <span key={i} style={{ display: 'block', color: e.st === 'err' ? 'var(--red, #DC2626)' : undefined }}>[{f(e.at)}] {e.step === 'end' ? 'End' : 'Step ' + e.step}: {e.msg}</span>)}</p>
}
