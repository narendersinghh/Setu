// Shared bits of the reports module.
import { toast } from '@/components/ui'

export interface ReportItem {
  id: number; lead_id: number; token: string; views: number; created_at: string; has_pdf: boolean
  prompt_name: string | null; name: string; category: string; website: string
  web_score: number | null; map_score: number | null; stage: string; url: string; pdf_url: string
}

export interface Prompt { id: number; type: string; name: string; subject: string; content: string; is_active: boolean; is_default: boolean; updated_at: string }
export interface Asset { id: number; name: string; file: string; placement: string; prompt_id: number | null; prompt_name?: string | null; created_at: string }
export interface AssetsResp { assets: Asset[]; builtin: { key: string; label: string; uri: string }[]; places: { key: string; label: string }[]; prompts: Prompt[] }

const STAGES: Record<string, [string, string]> = { new: ['New', 'gray'], queued: ['Queued', 'blue'], auditing: ['Auditing', 'blue'], reporting: ['Writing report', 'purple'], report_ready: ['Report ready', 'gold'], contacting: ['Sending', 'blue'], contacted: ['Contacted', 'teal'], opened: ['Opened', 'teal'], replied: ['Replied', 'green'], won: ['Won', 'green'], lost: ['Not interested', 'red'], failed: ['Failed', 'red'], skipped: ['Skipped', 'gray'] }

export function StageBadge({ s }: { s: string }) { const a = STAGES[s] || [s, 'gray']; return <span className={'bdg bdg-' + a[1]}>{a[0]}</span> }

export function ScoreChip({ v }: { v: number | null }) {
  if (v === null || v === undefined) return <span className="muted">—</span>
  return <span className={'sc sc-' + (v >= 75 ? 'g' : v >= 50 ? 'a' : 'r')}>{v}</span>
}

export function copyText(s: string) {
  navigator.clipboard?.writeText(s).then(() => toast('Copied.'), () => toast(s))
}
