import { createContext, useContext, ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, setCsrf } from './api'

export type Role = 'super' | 'admin' | 'sales'
export interface Me {
  user: { id: number; name: string; email: string; phone: string; role: Role; platform_admin: boolean }
  org: { id: number; slug: string; name: string; plan: string; status: string }
  workspaces: { id: number; name: string; role: Role }[]
  csrf: string
  perms: string[]
  modules: Record<string, boolean>
  roles: Record<Role, [string, string, string]>
  settings: Record<string, string>
}

const Ctx = createContext<Me | null>(null)

export function useMeQuery() {
  return useQuery({ queryKey: ['me'], queryFn: async () => { const m = await api.get<Me>('/me'); setCsrf(m.csrf); return m }, retry: false, staleTime: 60_000 })
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
