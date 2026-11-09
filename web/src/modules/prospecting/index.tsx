import { WebModule } from '../types'
import { Guard } from '@/pages/Core'
import { CampaignPage, Prospects, Scraper } from './pages'
import { DataTab, OutreachTab, PricingTab } from './settings'

// prospecting module – Map Scraper, campaign results, Scraped Data and the Prospecting settings tabs.
const g = (el: JSX.Element) => <Guard perm="prospecting" module="prospecting">{el}</Guard>

const mod: WebModule = {
  routes: [
    { path: 'scraper', element: g(<Scraper />) },
    { path: 'campaigns', element: g(<Scraper />) },
    { path: 'campaigns/:id', element: g(<CampaignPage />) },
    { path: 'prospects', element: g(<Prospects />) },
  ],
  settings: [
    { key: 'data', group: 'prospecting', label: 'Map scraper & audits', ic: 'map', order: 10, component: DataTab },
    { key: 'pricing', group: 'prospecting', label: 'Pricing & proposal', ic: 'tag', order: 20, component: PricingTab },
    { key: 'outreach', group: 'prospecting', label: 'Outreach sending', ic: 'send', order: 40, component: OutreachTab },
  ],
}
export default mod
