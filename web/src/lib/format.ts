// Formatting helpers (same output as v1)
export function money(n: number | string | null | undefined, sym = '₹', dec = 0) {
  const v = Number(n || 0)
  return sym + v.toLocaleString('en-IN', { minimumFractionDigits: dec, maximumFractionDigits: dec })
}

export function num(n: number | string | null | undefined) { return Number(n || 0).toLocaleString('en-IN') }

export function ago(ts?: string | null) {
  if (!ts) return '—'
  const d = (Date.now() - new Date(ts).getTime()) / 1000
  if (d < 60) return 'just now'
  if (d < 3600) return Math.floor(d / 60) + 'm ago'
  if (d < 86400) return Math.floor(d / 3600) + 'h ago'
  if (d < 604800) return Math.floor(d / 86400) + 'd ago'
  return date(ts)
}

export function date(ts?: string | null) {
  if (!ts) return '—'
  return new Date(ts).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })
}

export function dateTime(ts?: string | null) {
  if (!ts) return '—'
  return new Date(ts).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/** Value for <input type="datetime-local"> */
export function toLocalInput(ts?: string | null) {
  if (!ts) return ''
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

export function initial(s?: string) { return (s || 'U').trim().charAt(0).toUpperCase() }

export function plural(n: number, w: string) { return n + ' ' + w + (n === 1 ? '' : 's') }

export function safeUrl(u?: string) { return u && /^https?:\/\//i.test(u) ? u : '#' }
