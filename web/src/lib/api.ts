// Tiny API client: JSON in/out, CSRF header on every change, typed errors.
let csrf = ''
export const setCsrf = (t: string) => { csrf = t || csrf }

export class ApiError extends Error {
  status: number
  constructor(status: number, msg: string) { super(msg); this.status = status }
}

async function req<T>(method: string, url: string, body?: unknown): Promise<T> {
  const isForm = typeof FormData !== 'undefined' && body instanceof FormData
  const res = await fetch('/api' + url, {
    method,
    credentials: 'same-origin',
    headers: { ...(body !== undefined && !isForm ? { 'Content-Type': 'application/json' } : {}), ...(method !== 'GET' ? { 'X-CSRF': csrf } : {}) },
    body: body === undefined ? undefined : isForm ? (body as FormData) : JSON.stringify(body),
  })
  const ct = res.headers.get('content-type') || ''
  const data = ct.includes('json') ? await res.json().catch(() => ({})) : await res.text()
  if (!res.ok) {
    if (res.status === 401 && !/^\/(login|signup|forgot|reset)/.test(location.pathname)) {
      location.href = '/login?next=' + encodeURIComponent(location.pathname + location.search)
    }
    throw new ApiError(res.status, (data && (data as any).error) || 'Request failed (' + res.status + ')')
  }
  if (data && typeof data === 'object' && 'csrf' in (data as any)) setCsrf((data as any).csrf)
  return data as T
}

export const api = {
  get: <T = any>(url: string, params?: Record<string, unknown>) => req<T>('GET', url + qs(params)),
  post: <T = any>(url: string, body?: unknown) => req<T>('POST', url, body ?? {}),
  put: <T = any>(url: string, body?: unknown) => req<T>('PUT', url, body ?? {}),
  patch: <T = any>(url: string, body?: unknown) => req<T>('PATCH', url, body ?? {}),
  del: <T = any>(url: string, body?: unknown) => req<T>('DELETE', url, body),
  /** Upload files: fields are appended to a FormData. */
  upload: <T = any>(url: string, fields: Record<string, string | Blob>) => {
    const f = new FormData()
    for (const k in fields) f.append(k, fields[k])
    return req<T>('POST', url, f)
  },
}

export function qs(p?: Record<string, unknown>) {
  if (!p) return ''
  const u = new URLSearchParams()
  for (const k in p) {
    const v = p[k]
    if (v === undefined || v === null || v === '') continue
    if (Array.isArray(v)) v.forEach(x => u.append(k, String(x)))
    else u.set(k, String(v))
  }
  const s = u.toString()
  return s ? '?' + s : ''
}

/** Download a file from an API URL (keeps the session cookie). */
export function download(url: string) { location.href = '/api' + url }
