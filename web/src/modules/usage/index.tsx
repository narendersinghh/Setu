import { WebModule } from '../types'
import { Guard } from '@/pages/Core'
import UsagePage, { UsageTab } from './UsagePage'

// usage module – API usage & costs page and its settings tab.
const mod: WebModule = {
  routes: [{ path: 'usage', element: <Guard perm="settings"><UsagePage /></Guard> }],
  settings: [{ key: 'usage', group: 'system', label: 'API usage & costs', ic: 'activity', order: 80, component: UsageTab }],
}
export default mod
