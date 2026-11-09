import { WebModule } from '../types'
import { Guard } from '@/pages/Core'
import Emails from './Emails'
import { EmailTab, WhatsAppTab, VoiceTab } from './SettingsTabs'

// messaging module – Emails page and the Messaging settings tabs. Lead-page pieces: ./LeadChannels.
const mod: WebModule = {
  routes: [{ path: 'emails', element: <Guard perm="leads"><Emails /></Guard> }],
  settings: [
    { key: 'email', group: 'channels', label: 'Email', ic: 'mail', order: 10, component: EmailTab },
    { key: 'whatsapp', group: 'channels', label: 'WhatsApp', ic: 'wa', order: 11, component: WhatsAppTab },
    { key: 'voice', group: 'channels', label: 'AI voice calls', ic: 'mic', order: 12, component: VoiceTab },
  ],
}
export default mod
