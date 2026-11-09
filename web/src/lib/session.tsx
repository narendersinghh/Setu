import { createContext, useContext, ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, setCsrf } from './api'

export type Role = 'super' | 'admin' | 'sales'
export type PlanStatus = 'free' | 'trial' | 'active' | 'expired'
export interface Org {
  id: number; slug: string; name: string; plan: { id: number; name: string } | string | null; status: string
  /** v2 billing: free | trial | active | expired (missing on older servers) */
  plan_status?: PlanStatus | string; plan_expires_at?: string | null; trial_ends_at?: string | null
}
export interface Limit { limit: number; used: number }
export interface Me {
  user: { id: number; name: string; email: string; phone: string; role: Role; platform_admin: boolean }
  org: Org
  workspaces: { id: number; name: string; role: Role }[]
  csrf: string
  perms: string[]
  modules: Record<string, boolean>
  roles: Record<Role, [string, string, string]>
  settings: Record<string, string>
  /** v2: usage against plan limits, e.g. {leads: {limit: 1000, used: 120}} (missing / 0 = unlimited). */
  limits?: Record<string, Limit>
  /** v2: a platform admin opened this workspace for support. */
  support?: boolean
  /** v2 billing: a payment gateway is enabled, so plans can be bought online. */
  billing?: boolean
}

/** /api/me as returned by the server: org is null for a platform-console session. */
export type MeRaw = Omit<Me, 'org'> & { org: Org | null }

/** Plan name of the workspace (v2 object or v1 string). */
export const planName = (o: Org) => (o.plan && typeof o.plan === 'object' ? o.plan.name : o.plan) || ''

const Ctx = createContext<Me | null>(null)

export function useMeQuery() {
  return useQuery({ queryKey: ['me'], queryFn: async () => { const m = await api.get<MeRaw>('/me'); setCsrf(m.csrf); return m }, retry: false, staleTime: 60_000 })
}

export function SessionProvider({ me, children }: { me: Me; children: ReactNode }) {
  return <Ctx.Provider value={me}>{children}</Ctx.Provider>
}

export function useMe(): Me {
  const m = useContext(Ctx)
  if (!m) throw new Error('useMe outside SessionProvider')
  return m
}

/** Permission check (same model as v1: super, admin, sales). */
export function useCan() {
  const m = useMe()
  return (p: string) => m.perms.includes(p)
}

/** Module switched on? CRM is always on. */
export function useMod() {
  const m = useMe()
  return (k: string) => k === 'crm' || !!m.modules[k]
}

export function useRefreshMe() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: ['me'] })
}

/** Whole days until a date (negative when past); null without a date. */
export function daysLeft(ts?: string | null) {
  if (!ts) return null
  const t = new Date(ts).getTime()
  if (!Number.isFinite(t)) return null
  return Math.ceil((t - Date.now()) / 86_400_000)
}
