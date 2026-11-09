// Settings → Prospecting: Map scraper & audits, Pricing & proposal, Outreach sending (v1 settings.php $T).
import { FormEvent, useState } from 'react'
import { api } from '@/lib/api'
import { Btn, Icon, Sec } from '@/components/ui'
import { FieldDef, SettingsForm } from '@/components/SettingsForm'

const yn = { '1': 'Yes', '0': 'No' }

export function DataTab() {
  const f: FieldDef[] = [
    ['maps_source', 'Default Google Maps source', 'select', { places: 'Google Places API (New) – up to 60 results', serpapi: 'SerpAPI Google Maps – 100+ results' }],
    ['google_api_key', 'Google Cloud API key', 'secret', null, 'Enable "Places API (New)" (and optionally "PageSpeed Insights API") at console.cloud.google.com → APIs & Services, and make sure billing is enabled.'],
    ['serpapi_key', 'SerpAPI key', 'secret', null, 'serpapi.com → Dashboard → API key. Needed for more than 60 listings per search.'],
    ['maps_skip_existing', 'Skip businesses already in your data', 'select', { '1': 'Yes – never add the same business twice (recommended)', '0': 'No – each search keeps its own full list' }, 'Repeating a search (e.g. dentists in Baner) adds only NEW businesses. A free IDs-only Google request checks each page first, so businesses you already have cost no API hits.'],
    ['maps_atmosphere', 'Fetch business descriptions (Google editorial summary)', 'select', { '0': 'No – cheaper (Text Search Enterprise, $35 / 1,000 requests)', '1': 'Yes – adds the description check to audits (Enterprise + Atmosphere, $40 / 1,000)' }],
    ['maps_language', 'Language code'],
    ['maps_region', 'Region code (e.g. in, us, ae)'],
    ['default_country_code', 'Default country calling code'],
    ['pagespeed', 'Run Google PageSpeed test (Core Web Vitals)', 'select', { '0': 'No (faster)', '1': 'Yes (adds ~20-40s per site)' }],
    ['deep_crawl', 'Crawl inner pages & check broken links', 'select', { '1': 'Yes (recommended)', '0': 'No (faster)' }],
  ]
  return <>
    <SettingsForm fields={f} defaults={{ maps_source: 'places', maps_skip_existing: '1', maps_atmosphere: '0', maps_language: 'en', maps_region: 'in', default_country_code: '91', pagespeed: '0', deep_crawl: '1' }} />
    <TestMaps />
  </>
}

/** Step-by-step check of the Google key (free IDs-only request, then 1 billable request). */
function TestMaps() {
  const [city, setCity] = useState('')
  const [busy, setBusy] = useState(false)
  const [lines, setLines] = useState<string[]>([])
  const run = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const r = await api.post<{ lines: string[] }>('/prospecting/test-maps', { city }); setLines(r.lines) } catch (err: any) { setLines(['✕ ' + err.message]) } finally { setBusy(false) }
  }
  return <form className="mt" onSubmit={run}>
    <Sec ic="search" c="orange">Test your data source</Sec>
    <div className="row nw"><input value={city} onChange={e => setCity(e.target.value)} placeholder="City for the test (Pune)" style={{ margin: 0 }} /><Btn type="submit" ic="search" busy={busy}>Run test</Btn></div>
    {lines.length > 0 && <div className={'al mt ' + (lines.some(l => l.startsWith('✕')) ? 'al-err' : 'al-ok')}><Icon n={lines.some(l => l.startsWith('✕')) ? 'alert' : 'check'} /><span>{lines.map((l, i) => <div key={i}>{l}</div>)}</span></div>}
  </form>
}

export function PricingTab() {
  const f: FieldDef[] = [
    ['yearly_fee', 'Yearly SEO & Google Maps package fee', 'number'],
    ['website_fee', 'Website development fee (one time)', 'number'],
    ['services', 'Services you offer (one per line)', 'textarea'],
    ['proposal_pdf', 'Website proposal PDF', 'file', { '.pdf': '' }, 'Attached with the report when a business has no website.'],
  ]
  return <SettingsForm fields={f} defaults={{ yearly_fee: '72000', website_fee: '25000' }} />
}

export function OutreachTab() {
  const f: FieldDef[] = [
    ['auto_contact', 'Send the report automatically when ready (default)', 'select', { '1': 'Yes', '0': 'No – I will review and send' }],
    ['send_email', 'Send by email', 'select', yn],
    ['send_whatsapp', 'Send by WhatsApp', 'select', yn],
    ['send_gap', 'Seconds between two sends', 'number', null, 'Spreads sending out to protect your email & WhatsApp reputation.'],
    ['daily_limit', 'Max businesses contacted per day', 'number'],
    ['skip_contacted', 'Skip businesses already contacted in another search', 'select', yn],
    ['promote_on_reply', 'Move scraped businesses to Leads when they reply', 'select', { '0': 'No – only when won or moved manually', '1': 'Yes' }],
  ]
  return <SettingsForm fields={f} defaults={{ auto_contact: '1', send_email: '1', send_whatsapp: '1', send_gap: '90', daily_limit: '60', skip_contacted: '1', promote_on_reply: '0' }} />
}
