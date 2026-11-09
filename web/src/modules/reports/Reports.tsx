// Audit Reports – AI growth reports sent to prospects (v1 app/pages/reports.php).
import { FormEvent, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ago } from '@/lib/format'
import { Badge, Card, Empty, Icon, Kpi, Spinner, confirmDo, toast, usePageTitle } from '@/components/ui'
import { ReportItem, StageBadge, ScoreChip, copyText } from './common'

interface ListResp { rows: ReportItem[]; kpi: { total: number; opened: number; views: number; week: number }; mode: string }

export default function Reports() {
  usePageTitle('Audit Reports', 'AI growth reports sent to prospects as branded PDFs')
  const [q, setQ] = useState('')
  const [search, setSearch] = useState('')
  const qc = useQueryClient()
  const r = useQuery({ queryKey: ['reports', search], queryFn: () => api.get<ListResp>('/reports', { q: search }) })
  const del = async (x: ReportItem) => {
    if (!confirmDo('Delete this report?')) return
    try { await api.del('/reports/' + x.id); toast('Report deleted.'); qc.invalidateQueries({ queryKey: ['reports'] }) } catch (e: any) { toast(e.message) }
  }
  const submit = (e: FormEvent) => { e.preventDefault(); setSearch(q.trim()) }
  if (!r.data) return <Spinner />
  const k = r.data.kpi
  const rows = r.data.rows
  return <>
    <div className="stats">
      <Kpi label="Reports created" value={Number(k.total)} ic="file" c="violet" foot={Number(k.week) + ' this week'} />
      <Kpi label="Opened by prospects" value={Number(k.opened)} ic="eye" c="teal" foot={Number(k.total) ? Math.round(Number(k.opened) / Number(k.total) * 100) + '% open rate' : '—'} />
      <Kpi label="Total views" value={Number(k.views)} ic="activity" c="sky" foot="report link opens" />
      <Kpi label="Design & prompts" value="Edit" ic="layers" c="amber" foot="colours, doodles, AI prompts" to="/settings/templates" />
    </div>
    <form className="fbar" onSubmit={submit}>
      <label className="srch"><Icon n="search" /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search by business name…" /></label>
      <button className="btn btn-n">Search</button>
      <Link className="btn" to="/settings/templates?t=design"><Icon n="image" />Report design</Link>
    </form>
    <Card p0>
      {!rows.length ? <Empty ic="file" title="No reports yet">Open a search, select leads and click <b>Initiate Process</b> to generate AI audit reports.</Empty> :
        <div className="tw"><table className="tbl"><thead><tr><th className="sn">#</th><th>Business</th><th>Type</th><th>Web</th><th>Maps</th><th>Prompt</th><th>Views</th><th>Stage</th><th>Created</th><th></th></tr></thead>
          <tbody>{rows.map((x, i) => <tr key={x.id}>
            <td className="sn">{i + 1}</td>
            <td><Link to={'/leads/' + x.lead_id}><b>{x.name}</b></Link><br /><small className="muted">{x.category}</small></td>
            <td>{x.website ? <Badge c="blue">Website + Maps</Badge> : <Badge c="amber">Maps + website pitch</Badge>}</td>
            <td><ScoreChip v={x.web_score} /></td>
            <td><ScoreChip v={x.map_score} /></td>
            <td className="sm">{x.prompt_name || '—'}</td>
            <td>{x.views}</td>
            <td><StageBadge s={x.stage} /></td>
            <td className="muted">{ago(x.created_at)}</td>
            <td className="act">
              <a className="ib" href={x.url} target="_blank" rel="noreferrer" title="View"><Icon n="eye" /></a>
              <a className="ib" href={'/api/reports/' + x.id + '/pdf'} title="Download"><Icon n="download" /></a>
              <button className="ib" title="Copy public link" onClick={() => copyText(x.url)}><Icon n="link" /></button>
              <button className="ib red" title="Delete" onClick={() => del(x)}><Icon n="trash" /></button>
            </td>
          </tr>)}</tbody></table></div>}
    </Card>
  </>
}
