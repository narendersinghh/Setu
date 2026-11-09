import { WebModule } from '../types'
import AIConnection from './AIConnection'

// ai-engine module – AI connection settings tab. The activity drawer lives in modules/reports/ActivityDrawer.tsx
// (lazy-loaded by the layout); the process log (ProcessModal, ProcessLog, LeadProgress) is in ./Process.
const mod: WebModule = {
  routes: [],
  settings: [{ key: 'ai', group: 'general', label: 'AI connection', ic: 'spark', order: 10, component: AIConnection }],
}
export default mod
