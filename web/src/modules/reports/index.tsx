import { WebModule } from '../types'
import { Guard } from '@/pages/Core'
import Reports from './Reports'
import Templates, { TemplatesPage } from './Templates'
import { BrandingTab, DeliveryTab } from './SettingsTabs'

// reports module: Audit Reports list, Templates & report design, brand / delivery settings.
// LeadReports (./LeadReports) is the lead-page card for the crm module.
const mod: WebModule = {
  routes: [
    { path: 'reports', element: <Guard perm="prospecting" module="prospecting"><Reports /></Guard> },
    { path: 'templates', element: <Guard perm="templates" module="prospecting"><TemplatesPage /></Guard> },
  ],
  settings: [
    { key: 'branding', group: 'general', label: 'Brand & PDF design', ic: 'image', order: 10, component: BrandingTab },
    { key: 'pdf', group: 'prospecting', label: 'Report delivery', ic: 'file', order: 30, component: DeliveryTab },
    { key: 'templates', group: 'prospecting', label: 'Templates & report design', ic: 'layers', order: 40, component: Templates },
  ],
}
export default mod
