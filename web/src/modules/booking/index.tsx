import { WebModule } from '../types'
import { Guard } from '@/pages/Core'
import Appointments from './Appointments'
import AppointmentsSettings from './SettingsTab'

// booking module: appointments calendar/list and the "Appointments & calendar" settings tab.
const mod: WebModule = {
  routes: [{ path: 'appointments', element: <Guard perm="appointments"><Appointments /></Guard> }],
  settings: [{ key: 'appointments', group: 'crm', label: 'Appointments & calendar', ic: 'cal', order: 40, component: AppointmentsSettings }],
}
export default mod
