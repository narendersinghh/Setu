// Settings tabs of the reports module: Brand & PDF design, Report delivery (v1 settings.php $T).
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Btn, Card, Field, Hi, Spinner, confirmDo, toast } from '@/components/ui'
import { FieldDef, SettingsForm, useSettings } from '@/components/SettingsForm'

interface FontsResp { fonts: string[]; custom: string[] }

export function BrandingTab() {
  const qc = useQueryClient()
  const f = useQuery({ queryKey: ['report-fonts'], queryFn: () => api.get<FontsResp>('/report-fonts') })
  const s = useSettings()
  if (!f.data) return <Spinner />
  const opts = Object.fromEntries(f.data.fonts.map(x => [x, x]))
  const fields: FieldDef[] = [
    ['brand_primary', 'Primary colour (report cover, booking page)', 'color'],
    ['brand_accent', 'Accent colour (buttons, highlights)', 'color'],
    ['brand_text', 'Text colour', 'color'],
    ['font_heading', 'Heading font', 'select', opts],
    ['font_body', 'Body font', 'select', opts],
  ]
  const upload = async (file?: File) => {
    if (!file) return
    try { const r = await api.upload('/report-fonts', { file }); toast(r.message); qc.invalidateQueries({ queryKey: ['report-fonts'] }) } catch (e: any) { toast(e.message) }
  }
  const remove = async (n: string) => {
    if (!confirmDo('Remove the font "' + n + '"?')) return
    try { await api.del('/report-fonts/' + encodeURIComponent(n)); toast('Font removed.'); qc.invalidateQueries({ queryKey: ['report-fonts'] }) } catch (e: any) { toast(e.message) }
  }
  const v = s.data || {}
  const P = v.brand_primary || '#0B0B0B', A = v.brand_accent || '#B8860B'
  return <>
    <SettingsForm fields={fields} defaults={{ brand_primary: '#0B0B0B', brand_accent: '#B8860B', brand_text: '#1A1A1A', font_heading: 'Inter', font_body: 'Inter' }}>
      <div className="fg">
        <Field label="Upload a brand font (.ttf)" hint="Upload a TrueType font (e.g. from Google Fonts). It then appears in the font lists.">
          <input type="file" accept=".ttf" onChange={e => { upload(e.target.files?.[0]); e.target.value = '' }} />
        </Field>
        {f.data.custom.length > 0 && <Field label="Your fonts"><span className="row wrap">{f.data.custom.map(n => <span key={n} className="row"><code>{n}</code><Btn xs v="ghost" ic="trash" title="Remove" onClick={() => remove(n)} /></span>)}</span></Field>}
      </div>
    </SettingsForm>
    <Card title={<><Hi n="eye" c="amber" />Preview</>}>
      <div className="bprev" style={{ background: P }}>{v.company_logo ? <img src={v.company_logo} alt="" style={{ height: 54 }} /> : null}<div><small style={{ color: A }}>DIGITAL GROWTH REVIEW</small><b>Website &amp; Google Maps Review</b><span style={{ color: A }}>Prepared for Sample Business</span></div></div>
      <div className="bprev" style={{ background: '#fff', border: '1px solid #E5E7EB', marginTop: 10 }}>{(v.company_logo_light || v.company_logo) ? <img src={v.company_logo_light || v.company_logo} alt="" style={{ height: 54, background: v.company_logo_light ? undefined : '#0B0B0B', borderRadius: 10, padding: v.company_logo_light ? 0 : '7px 10px' }} /> : null}<div><small style={{ color: A }}>ON WHITE</small><b style={{ color: '#0F172A' }}>Closing page &amp; emails</b><span className="muted">Logo for light backgrounds{v.company_logo_light ? '' : ' – not uploaded, using a badge'}</span></div></div>
      <p className="hint">Illustrations and a full PDF preview: <Link className="lnk" to="/settings/templates?t=design">Templates &amp; Design → Report design</Link></p>
    </Card>
  </>
}

const DELIVERY: FieldDef[] = [
  ['report_delivery', 'How should businesses receive their audit report?', 'select', { both: 'Both – web link in the message + PDF attached (recommended)', link: 'Web link only – a beautiful online report page (no PDF)', pdf: 'PDF only – attached to email & WhatsApp' }],
  ['report_cta', 'Button text on the online report', 'text', null, 'Shown at the top of the web report, e.g. “Book a free strategy call”.'],
  ['ai_batch', 'Bulk reports (several businesses at once)', 'select', { parallel: 'Fast – several reports written at the same time (any AI provider, recommended)', batch: 'Cheapest – Claude Message Batches, half price (usually minutes, Claude only)', off: 'Off – write every report one by one' }, 'Fast: one background worker keeps several AI requests running at once and saves each report the moment it is ready – works with Claude, ChatGPT, Gemini, OpenRouter, Groq, DeepSeek and custom endpoints. Cheapest: Claude writes the whole run as one batch at 50% of the AI cost; with other providers Fast is used.'],
  ['ai_parallel', 'Reports written at the same time', 'select', Object.fromEntries(Array.from({ length: 10 }, (_, i) => [String(i + 1), String(i + 1)])), 'Default 5. Lower it if your AI account hits rate limits (the engine waits and retries automatically).'],
  ['batch_min', 'Treat as a bulk run from this many reports at once', 'number', null, 'Default 4. Single reports are always written straight away.'],
]

export function DeliveryTab() {
  return <SettingsForm fields={DELIVERY} defaults={{ report_delivery: 'both', report_cta: 'Book a free strategy call', ai_batch: 'parallel', ai_parallel: '5', batch_min: '4' }} />
}
