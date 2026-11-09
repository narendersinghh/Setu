import { lazy, Suspense } from 'react'
import { WebModule } from '../types'
import { Guard } from '@/pages/Core'
import { Spinner } from '@/components/ui'

// crm module – dashboard, leads (list & board), lead detail, new lead / import, activity, custom fields.
const Dashboard = lazy(() => import('./Dashboard'))
const Leads = lazy(() => import('./Leads'))
const Lead = lazy(() => import('./Lead'))
const LeadNew = lazy(() => import('./LeadNew'))
const Activity = lazy(() => import('./Activity'))
const FieldsTab = lazy(() => import('./FieldsTab'))

const S = ({ children }: { children: JSX.Element }) => <Suspense fallback={<Spinner />}>{children}</Suspense>

const mod: WebModule = {
  routes: [
    { path: 'dashboard', element: <Guard perm="dashboard"><S><Dashboard /></S></Guard> },
    { path: 'activity', element: <Guard perm="dashboard"><S><Activity /></S></Guard> },
    { path: 'leads', element: <Guard perm="leads"><S><Leads /></S></Guard> },
    { path: 'leads/new', element: <Guard perm="leads"><S><LeadNew /></S></Guard> },
    { path: 'leads/:id', element: <Guard perm="leads"><S><Lead /></S></Guard> },
  ],
  settings: [{ key: 'fields', group: 'crm', label: 'Custom fields', ic: 'list', order: 20, component: () => <S><FieldsTab /></S> }],
}
export default mod
