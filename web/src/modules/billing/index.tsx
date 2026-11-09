import { WebModule } from '../types'
import { Guard } from '@/pages/Core'
import BillingPage from './BillingPage'

// billing module – plan, limits, upgrades (payment gateways) and payment history.
const mod: WebModule = {
  routes: [{ path: 'billing', element: <Guard perm="settings"><BillingPage /></Guard> }],
  settings: [{ key: 'billing', group: 'system', label: 'Plan & billing', ic: 'crown', order: 70, component: () => <BillingPage embedded /> }],
}
export default mod
