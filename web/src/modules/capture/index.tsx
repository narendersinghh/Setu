import { WebModule } from '../types'
import CaptureTab from './CaptureTab'

// capture module – lead sources (website form, embed script, Meta & Google Ads, webhooks) in Settings → CRM.
const mod: WebModule = {
  routes: [],
  settings: [{ key: 'capture', group: 'crm', label: 'Lead capture', ic: 'plug', order: 20, component: CaptureTab }],
}
export default mod
