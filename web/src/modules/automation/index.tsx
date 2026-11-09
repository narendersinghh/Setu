import { Guard } from '@/pages/Core'
import { WebModule } from '../types'
import Automations from './List'
import Editor from './Editor'
import Recipes from './Recipes'

// automation module – routes. The lead page can embed ./LeadAutomations (runs of a lead).
const g = (el: JSX.Element) => <Guard perm="automations">{el}</Guard>

const mod: WebModule = {
  routes: [
    { path: 'automations', element: g(<Automations />) },
    { path: 'automations/new', element: g(<Editor key="new" />) },
    { path: 'automations/recipes', element: g(<Recipes />) },
    { path: 'automations/:id', element: g(<Editor />) },
  ],
  settings: [],
}
export default mod
