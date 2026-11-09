package reports

// The report document: A4 landscape slides in HTML/CSS for Chromium (Gotenberg for the PDF, the
// visitor's browser for the web report). Port of v1 Report::modern().

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

// Getter reads workspace settings (*settings.Store implements it).
type Getter interface {
	Get(k, def string) string
}

// Competitor: another business of the same Maps search.
type Competitor struct {
	Name     string   `db:"name" json:"name"`
	Rating   *float64 `db:"rating" json:"rating"`
	Reviews  *int     `db:"reviews" json:"reviews"`
	Position int      `db:"position" json:"position"`
	Website  string   `db:"website" json:"website"`
}

// RenderIn is everything needed to draw a report.
type RenderIn struct {
	Lead    *shared.Lead
	Keyword string // search phrase analysed (campaign keyword or category)
	Audit   J      // leads.audit
	Data    J      // the AI-written report (reports.html)
	HasWeb  bool
	Comp    []Competitor
	S       Getter
	Now     time.Time // workspace time zone
	// Art returns the illustration of a section (uploaded doodle or built-in), as a URL / data URI.
	Art func(key string) string
	// Logo returns the logo for a background colour and an optional contrasting chip colour.
	Logo func(bg string) (uri, chip string)
	// FontURL maps a built-in font file name to its URL ("" = same name, sent as a PDF asset).
	FontURL func(file string) string
	// CustomFonts: uploaded brand fonts (family → URL / data URI).
	CustomFonts map[string]string
}

var builtinFonts = []struct {
	Family string
	Weight int
	File   string
}{
	{"Inter", 400, "Inter-Regular.ttf"}, {"Inter", 500, "Inter-Medium.ttf"}, {"Inter", 600, "Inter-SemiBold.ttf"}, {"Inter", 700, "Inter-Bold.ttf"},
	{"Inter Tight", 600, "InterTight-SemiBold.ttf"}, {"Inter Tight", 700, "InterTight-Bold.ttf"}, {"Inter Tight", 800, "InterTight-ExtraBold.ttf"},
}

// stdFonts are the v1 font choices that map to the built-in Inter / Inter Tight.
var stdFonts = map[string]bool{"": true, "Inter": true, "DejaVu Sans": true, "Helvetica": true, "Times-Roman": true, "Courier": true}

var areaStyle = map[string][2]string{
	"Google Maps": {"#EF4444", "maps"}, "Reviews": {"#F59E0B", "reviews"}, "Website": {"#3B82F6", "website"}, "Speed": {"#8B5CF6", "speed"},
	"Content": {"#14B8A6", "content"}, "Conversion": {"#EC4899", "conversion"}, "Trust": {"#6366F1", "trust"}, "Social": {"#0EA5E9", "social"},
}

func impactRank(x J) int {
	switch strings.ToLower(x.S("impact")) {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	}
	return 3
}

// sortedFindings: findings with a title, most costly first.
func sortedFindings(d J) []J {
	var F []J
	for _, x := range d.Objects("findings") {
		if x.S("title") != "" {
			F = append(F, x)
		}
	}
	sort.SliceStable(F, func(i, j int) bool { return impactRank(F[i]) < impactRank(F[j]) })
	return F
}

// auditChecks: Maps checks then website checks (when the site is up), each tagged with its source.
func auditChecks(a J, hasWeb bool) []J {
	var out []J
	add := func(part, src string) {
		for _, c := range a.M(part).Objects("checks") {
			cc := J{}
			for k, v := range c {
				cc[k] = v
			}
			cc["src"] = src
			out = append(out, cc)
		}
	}
	add("map", "Google Maps")
	if hasWeb {
		add("website", "Website")
	}
	return out
}

func catScores(a J, hasWeb bool) map[string]float64 {
	m := map[string]float64{}
	if hasWeb {
		for k, v := range a.M("website").M("cats") {
			m[k] = num(v)
		}
	}
	for k, v := range a.M("map").M("cats") {
		m[k] = num(v)
	}
	return m
}

// Render builds the full HTML document of a report.
func Render(in *RenderIn) string {
	S := in.S.Get
	l, a, d := in.Lead, in.Audit, in.Data
	if a == nil {
		a = J{}
	}
	if d == nil {
		d = J{}
	}
	P := safeColor(S("brand_primary", "#0B0B0B"), "#0B0B0B")
	A := safeColor(S("brand_accent", "#B8860B"), "#B8860B")
	cur := S("currency_symbol", "₹")
	money := func(v float64) string { return esc(cur) + numFmt(v) }
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	var ws, ms *int
	if in.HasWeb && l.WebScore != nil {
		v := int(*l.WebScore)
		ws = &v
	}
	if l.MapScore != nil {
		v := int(*l.MapScore)
		ms = &v
	}
	checks := auditChecks(a, in.HasWeb)
	cnt := map[string]int{}
	for _, c := range checks {
		cnt[c.S("status")]++
	}
	F := sortedFindings(d)
	high := 0
	for _, x := range F {
		if impactRank(x) == 0 {
			high++
		}
	}
	op := d.M("opportunity")
	cv, ce, pe, cr := op.N("customer_value"), op.N("current_enquiries_month"), op.N("potential_enquiries_month"), op.N("conversion_rate")
	if cr > 1 {
		cr /= 100
	}
	if cr <= 0 {
		cr = .2
	}
	missed := max(0, int(math.Round(pe-ce)))
	rev := math.Round(float64(missed) * 12 * cr * cv)
	logoFor := func(bg string) (string, string) {
		if in.Logo == nil {
			return "", ""
		}
		return in.Logo(bg)
	}
	hasLogo := func() bool { u1, _ := logoFor(P); u2, _ := logoFor("#FFFFFF"); return u1 != "" || u2 != "" }()
	logoImg := func(bg, style, cls string) string {
		u, chip := logoFor(bg)
		if u == "" {
			return ""
		}
		c := ""
		if cls != "" {
			c = ` class="` + cls + `"`
		}
		i := `<img` + c + ` src="` + esc(u) + `" style="` + style + `" alt="">`
		if chip != "" {
			return `<span style="display:inline-block;background:` + chip + `;border-radius:10px;padding:7px 10px;line-height:0">` + i + `</span>`
		}
		return i
	}
	co := S("company_name", "")
	web := bareHost(S("company_website", ""))
	col := func(area string) string {
		if s, ok := areaStyle[area]; ok {
			return s[0]
		}
		return "#64748B"
	}
	ic := func(area string) string {
		g := "content"
		if s, ok := areaStyle[area]; ok {
			g = s[1]
		}
		return `<img class="gl" src="` + chartGlyph(g, "#FFFFFF", 22) + `" alt="">`
	}
	imp := func(i string) string {
		switch strings.ToLower(i) {
		case "high":
			return "bad"
		case "low":
			return "ok"
		}
		return "mid"
	}
	pg := 0
	slide := func(kick, title, sub, body, cls string) string {
		pg++
		s := `<section class="slide ` + cls + `"><header class="sh"><div><span class="kick">` + t(kick, 0) + `</span><h2>` + t(title, 95) + `</h2>`
		if sub != "" {
			s += `<p class="sub">` + t(sub, 180) + `</p>`
		}
		return s + `</div><span class="pno">` + fmt.Sprintf("%02d", pg) + `</span></header><div class="sb">` + body + `</div></section>`
	}
	var o strings.Builder

	// ---- Cover
	chips := ""
	for _, x := range []struct {
		lb string
		v  *int
	}{{"Website", ws}, {"Google Maps", ms}} {
		if x.v != nil && *x.v < 80 {
			chips += `<div class="sc"><img src="` + chartGauge(*x.v, "", 120) + `" alt=""><span>` + x.lb + ` score</span></div>`
		}
	}
	o.WriteString(`<section class="slide cover"><div class="cv-l"><div class="cv-top">`)
	if hasLogo {
		o.WriteString(logoImg(P, "", "cv-logo"))
	} else {
		o.WriteString(`<b class="cv-co">` + esc(co) + `</b>`)
	}
	sub := "Google Maps audit &amp; website plan"
	if in.HasWeb {
		sub = "Website &amp; Google Maps audit"
	}
	o.WriteString(`</div><div class="cv-mid"><span class="pill">Growth audit · ` + now.Format("January 2006") + `</span><h1>` + t(l.Name, 60) + `</h1><p class="cv-s">` + sub + `</p><p class="cv-a">` +
		t(joinNonEmpty(" · ", l.Category, l.City), 80) + `<br>` + t(l.Address, 90) + `</p></div><div class="cv-toc">`)
	for _, x := range [][2]string{{"01", "Summary"}, {"02", "Problem map"}, {"03", "Market research"}, {"04", "Findings"}, {"05", "90-day plan"}} {
		o.WriteString(`<div><b>` + x[0] + `</b><span>` + x[1] + `</span></div>`)
	}
	o.WriteString(`</div><div class="cv-by">Prepared by <b>` + esc(co) + `</b> · ` + esc(S("company_phone", "")) + ` · ` + esc(web) + `</div></div>`)
	o.WriteString(`<div class="cv-r"><span class="lbl">At a glance</span><p class="cv-h">` + t(d.S("headline"), 140) + `</p><div class="kpis">` +
		`<div class="k bad"><b>` + strconv.Itoa(len(F)) + `</b><span>problems holding you back</span></div>` +
		`<div class="k mid"><b>` + strconv.Itoa(high) + `</b><span>with a high impact on enquiries</span></div>` +
		`<div class="k ok"><b>` + strconv.Itoa(len(d.L("quick_wins"))) + `</b><span>quick wins for this week</span></div>`)
	if missed > 0 {
		o.WriteString(`<div class="k pri"><b>+` + strconv.Itoa(missed) + `</b><span>enquiries a month are realistic</span></div>`)
	}
	o.WriteString(`</div>`)
	if chips != "" {
		o.WriteString(`<div class="scs">` + chips + `</div>`)
	}
	o.WriteString(`</div></section>`)

	// ---- Executive summary
	sum := ""
	n := 0
	for _, p := range strings.Split(d.S("summary"), "\n\n") {
		if strings.TrimSpace(p) == "" || n >= 2 {
			continue
		}
		n++
		sum += `<p>` + t(p, 380) + `</p>`
	}
	top := ""
	for i, x := range F[:min(3, len(F))] {
		top += `<div class="tp" style="--c:` + col(x.S("area")) + `"><span class="n">` + strconv.Itoa(i+1) + `</span><div><b>` + t(x.S("title"), 60) + `</b><p>` + t(x.S("why"), 150) + `</p></div></div>`
	}
	str := ""
	for _, s := range first(d.Strings("strengths"), 3) {
		str += `<li>` + t(s, 60) + `</li>`
	}
	b := `<div class="g g2"><div class="lead">` + sum
	if missed > 0 && rev > 0 {
		b += `<div class="g g2 tight"><div class="tile bad"><span>Missed enquiries</span><b>~` + strconv.Itoa(missed) + ` / month</b><small>realistic gap today</small></div><div class="tile acc"><span>Revenue at stake</span><b>` + money(rev) + `</b><small>per year (estimate)</small></div></div>`
	}
	if str != "" {
		b += `<div class="good"><span class="lbl ok">Already working</span><ul>` + str + `</ul></div>`
	}
	b += `</div><div><span class="lbl bad">The 3 problems costing you most</span>` + top + `</div></div>`
	o.WriteString(slide("Executive summary", "What is holding "+l.Name+" back", "", b, ""))

	// ---- Problem map
	type area struct {
		k    string
		f, w int
	}
	var areas []*area
	idx := map[string]*area{}
	for _, c := range checks {
		k := c.S("src")
		if v, ok := c["cat"]; ok && v != nil {
			k = str(v)
		}
		ar := idx[k]
		if ar == nil {
			ar = &area{k: k}
			idx[k] = ar
			areas = append(areas, ar)
		}
		switch c.S("status") {
		case "fail":
			ar.f++
		case "warn":
			ar.w++
		}
	}
	var ar2 []*area
	for _, x := range areas {
		if x.f+x.w > 0 {
			ar2 = append(ar2, x)
		}
	}
	sort.SliceStable(ar2, func(i, j int) bool { return ar2[i].f*2+ar2[i].w > ar2[j].f*2+ar2[j].w })
	ar2 = ar2[:min(8, len(ar2))]
	mx := 1
	for _, x := range ar2 {
		mx = max(mx, x.f+x.w)
	}
	bars := ""
	for _, x := range ar2 {
		bars += fmt.Sprintf(`<div class="br"><span>%s</span><div><i class="f" style="width:%d%%"></i><i class="w" style="width:%d%%"></i></div><b>%d</b></div>`,
			t(x.k, 24), int(math.Round(float64(x.f)/float64(mx)*100)), int(math.Round(float64(x.w)/float64(mx)*100)), x.f+x.w)
	}
	cats := lowCats(a, in.HasWeb, 6)
	cb := ""
	for _, c := range cats {
		cls := " a"
		if c.V < 50 {
			cls = " r"
		}
		cb += fmt.Sprintf(`<div class="br sc%s"><span>%s</span><div><i style="width:%d%%"></i></div><b>%d</b></div>`, cls, t(c.K, 24), int(c.V), int(c.V))
	}
	var bad []J
	for _, c := range checks {
		if c.S("status") == "fail" {
			bad = append(bad, c)
		}
	}
	sort.SliceStable(bad, func(i, j int) bool { return bad[i].N("w") > bad[j].N("w") })
	bl := ""
	for _, c := range bad[:min(6, len(bad))] {
		bl += `<li><i class="x"></i><div><b>` + t(c.S("label"), 50) + `</b> <small>` + esc(c.S("src")) + `</small><p>` + t(c.S("detail"), 85) + `</p></div></li>`
	}
	if bars == "" {
		bars = `<p>No issues found.</p>`
	}
	if bl == "" {
		bl = `<li>No critical problems found.</li>`
	}
	b = `<div class="g g2"><div><div class="card"><span class="lbl">Issues by area <i class="lg f"></i>problems <i class="lg w"></i>warnings</span>` + bars + `</div>`
	if cb != "" {
		b += `<div class="card mt"><span class="lbl">Areas scoring below 80 / 100</span>` + cb + `</div>`
	}
	b += `</div><div class="card"><span class="lbl bad">Most serious problems</span><ul class="xl">` + bl + `</ul></div></div>`
	o.WriteString(slide("Problem map", "Where you are losing visibility and customers", fmt.Sprintf("%d problems and %d warnings found in %d checks", cnt["fail"], cnt["warn"], len(checks)), b, ""))

	// ---- Market research
	m, j := d.M("market"), d.M("journey")
	stats := ""
	for i, s := range first(m.Objects("stats"), 4) {
		stats += fmt.Sprintf(`<div class="st c%d"><b>%s</b><p>%s</p><small>%s</small></div>`, i, t(s.S("value"), 10), t(s.S("label"), 62), t(s.S("source"), 40))
	}
	fun := ""
	fi := 0
	for _, x := range [][2]string{{"Searches / month", "monthly_searches"}, {"See your profile", "profile_views"}, {"Call, visit or click", "actions"}, {"Become customers", "customers"}} {
		if v := j.N(x[1]); v > 0 {
			fun += fmt.Sprintf(`<div class="fn" style="width:%d%%"><b>%s</b><span>%s</span></div>`, max(34, 100-fi*18), numFmt(v), x[0])
			fi++
		}
	}
	tr := ""
	for i, x := range first(d.Objects("industry_trends"), 3) {
		tr += fmt.Sprintf(`<li><span class="n s">%d</span><div><b>%s</b><p>%s</p></div></li>`, i+1, t(x.S("title"), 38), t(x.S("detail"), 88))
	}
	catName := l.Category
	if catName == "" {
		catName = "business"
	}
	title := "How customers choose a " + trunc(strings.ToLower(catName), 30)
	if l.City != "" {
		title += " in " + l.City
	}
	msub := ""
	if st := m.S("search_term"); st != "" {
		msub = "Main search: “" + trunc(st, 60) + "”"
		if v := m.N("monthly_searches"); v > 0 {
			msub += " · about " + numFmt(v) + " searches a month (estimate)"
		}
	}
	b = `<div class="stats">` + stats + `</div><div class="g g3 mt"><div class="card"><span class="lbl">Monthly customer journey</span><div class="funnel">` + fun + `</div><small class="note">` + t(j.S("note"), 100) + `</small></div>` +
		`<div class="card soft"><span class="lbl">How your customers decide</span><p class="big-p">` + t(m.S("insight"), 220) + `</p></div>` +
		`<div class="card"><span class="lbl">Industry trends to act on</span><ul class="nl">` + tr + `</ul></div></div>`
	o.WriteString(slide("Market research", title, msub, b, ""))

	// ---- Competition
	if len(in.Comp) > 0 {
		type row struct {
			c  Competitor
			me bool
		}
		all := []row{{Competitor{Name: l.Name + " (you)", Rating: l.Rating, Reviews: l.Reviews, Position: l.Position, Website: l.Website}, true}}
		for _, c := range in.Comp[:min(5, len(in.Comp))] {
			all = append(all, row{c, false})
		}
		mr := 1
		for _, r := range all {
			mr = max(mr, deref(r.c.Reviews))
		}
		rows := ""
		for _, r := range all {
			cls := ""
			if r.me {
				cls = "me"
			}
			rt := "–"
			if r.c.Rating != nil && *r.c.Rating != 0 {
				rt = esc(strconv.FormatFloat(*r.c.Rating, 'f', -1, 64)) + " ★"
			}
			wb := `<span class="no">No</span>`
			if r.c.Website != "" {
				wb = "Yes"
			}
			rv := deref(r.c.Reviews)
			rows += fmt.Sprintf(`<tr class="%s"><td>#%d</td><td><b>%s</b></td><td>%s</td><td><div class="rb"><i style="width:%d%%"></i><span>%d</span></div></td><td>%s</td></tr>`,
				cls, r.c.Position, t(r.c.Name, 36), rt, int(math.Round(float64(rv)/float64(mr)*100)), rv, wb)
		}
		b = `<div class="g g32"><div class="card"><table class="tb"><thead><tr><th>Rank</th><th>Business</th><th>Rating</th><th>Reviews</th><th>Website</th></tr></thead><tbody>` + rows + `</tbody></table></div><div class="quote">` + t(d.S("competitors_insight"), 330) + `</div></div>`
		o.WriteString(slide("Competition", "Who customers pick instead of you", "Google Maps results for “"+in.Keyword+"”", b, ""))
	}

	// ---- Findings – 3 per slide
	F9 := F[:min(9, len(F))]
	for pi := 0; pi*3 < len(F9); pi++ {
		rows := ""
		for i, x := range F9[pi*3 : min(pi*3+3, len(F9))] {
			rows += `<div class="fd" style="--c:` + col(x.S("area")) + `"><div class="fd-h"><span class="ib">` + ic(x.S("area")) + `</span><span class="lbl c">` + t(x.S("area"), 20) + ` · #` + strconv.Itoa(pi*3+i+1) + `</span><h3>` + t(x.S("title"), 52) + `</h3>` +
				`<div class="tags"><span class="tag ` + imp(x.S("impact")) + `">Impact: ` + t(x.S("impact"), 8) + `</span><span class="tag ef">Effort: ` + t(x.S("effort"), 8) + `</span></div></div>` +
				`<div class="fd-c"><span class="lbl">What we found</span><p>` + t(x.S("found"), 160) + `</p></div>` +
				`<div class="fd-c"><span class="lbl bad">What it costs you</span><p>` + t(x.S("why"), 150) + `</p></div>` +
				`<div class="fd-c fix"><span class="lbl ok">The fix</span><p>` + t(x.S("fix"), 150) + `</p></div></div>`
		}
		k, h := "Detailed findings", "What is wrong – and exactly how to fix it"
		if pi > 0 {
			k, h = "Detailed findings (continued)", "More problems to fix"
		}
		o.WriteString(slide(k, h, "", rows, "fds"))
	}

	// ---- Checklist
	var need []J
	for _, c := range checks {
		if c.S("status") != "pass" {
			need = append(need, c)
		}
	}
	sort.SliceStable(need, func(i, j int) bool {
		fi, fj := need[i].S("status") != "fail", need[j].S("status") != "fail"
		if fi != fj {
			return !fi
		}
		return need[i].N("w") > need[j].N("w")
	})
	for i := 0; i*14 < len(need); i++ {
		li := ""
		for _, c := range need[i*14 : min(i*14+14, len(need))] {
			cls := "w"
			if c.S("status") == "fail" {
				cls = "x"
			}
			li += `<li><i class="` + cls + `"></i><div><b>` + t(c.S("label"), 50) + `</b> <small>` + t(c.S("src"), 14) + `</small><p>` + t(c.S("detail"), 80) + `</p></div></li>`
		}
		h, s := "Everything that needs attention", fmt.Sprintf("%d items to fix · %d other checks passed", len(need), cnt["pass"])
		if i > 0 {
			h, s = h+" (continued)", ""
		}
		o.WriteString(slide("Checklist", h, s, `<ul class="xl two">`+li+`</ul>`, ""))
	}

	// ---- Potential (revenue opportunity & ROI)
	oc := ""
	for _, x := range first(d.Objects("outcomes"), 4) {
		oc += `<tr><td><b>` + t(x.S("metric"), 40) + `</b></td><td>` + t(x.S("today"), 25) + `</td><td class="up">` + t(x.S("target"), 25) + `</td><td>` + t(x.S("timeframe"), 15) + `</td></tr>`
	}
	bmax := math.Max(1, math.Max(pe, ce))
	tiles := ""
	if cv > 0 {
		tiles += `<div class="tile"><span>Value of one customer</span><b>` + money(cv) + `</b><small>estimate</small></div>`
	}
	if rev > 0 {
		tiles += `<div class="tile ok"><span>Extra revenue / year</span><b>` + money(rev) + `</b><small>` + fmt.Sprintf("%d enquiries × %d%% × 12", missed, int(math.Round(cr*100))) + `</small></div>`
	}
	if pe > 0 {
		tiles += `<div class="tile pri"><span>Enquiries / month</span><b>` + numFmt(ce) + ` → ` + numFmt(pe) + `</b><small>today → achievable</small></div>`
	}
	fee := num(S("yearly_fee", "0"))
	if !in.HasWeb {
		fee += num(S("website_fee", "0"))
	}
	roi := ""
	if fee > 0 && cv > 0 {
		roi = `<small class="note" style="display:block;text-align:center;margin-top:12px"><b>` + strconv.Itoa(max(1, int(math.Ceil(fee/cv)))) + `</b> new customer(s) a year cover the cost of our work (break-even).</small>`
	}
	b = `<div class="g g32"><div><div class="card"><span class="lbl">Goals for the next 6 months</span><table class="tb"><thead><tr><th>Metric</th><th>Today</th><th>Target</th><th>When</th></tr></thead><tbody>` + oc + `</tbody></table></div><div class="g g3 tight mt">` + tiles + `</div></div>` +
		`<div class="card"><span class="lbl">Enquiries per month</span><div class="cols"><div><i style="height:` + strconv.Itoa(int(math.Round(ce/bmax*100))) + `%"></i><b>` + numFmt(ce) + `</b><span>Today (est.)</span></div><div class="ach"><i style="height:` + strconv.Itoa(int(math.Round(pe/bmax*100))) + `%"></i><b>` + numFmt(pe) + `</b><span>Achievable</span></div></div>` + roi + `</div></div>`
	o.WriteString(slide("Your potential", "What you can realistically achieve", op.S("assumptions"), b, ""))

	// ---- Roadmap
	if ph := first(d.Objects("plan"), 4); len(ph) > 0 {
		cols := ""
		for i, x := range ph {
			acts := ""
			for _, ac := range first(x.Strings("actions"), 3) {
				acts += `<li>` + t(ac, 62) + `</li>`
			}
			cols += fmt.Sprintf(`<div class="ph c%d"><span class="n">%d</span><span class="lbl">%s</span><h3>%s</h3><ul>%s</ul><p class="res"><b>Result:</b> %s</p></div>`, i, i+1, t(x.S("phase"), 20), t(x.S("title"), 36), acts, t(x.S("result"), 80))
		}
		o.WriteString(slide("Roadmap", "Your 90-day plan to fix it", "Starting with the changes that bring results fastest.", `<div class="road">`+cols+`</div>`, ""))
	}

	// ---- Next steps
	qw := ""
	for i, x := range first(d.Objects("quick_wins"), 5) {
		qw += fmt.Sprintf(`<li><span class="n s c%d">%d</span><div><b>%s</b><p>%s</p></div></li>`, i%4, i+1, t(x.S("title"), 45), t(x.S("how"), 120))
	}
	hw := ""
	for i, x := range first(d.Objects("how_we_help"), 3) {
		hw += fmt.Sprintf(`<div class="help c%d"><b>%s</b><p>%s</p><small><b>Result:</b> %s</small></div>`, i, t(x.S("service"), 45), t(x.S("what"), 130), t(x.S("result"), 70))
	}
	o.WriteString(slide("Next steps", "Start this week – and where we can help", "", `<div class="g g2"><div class="card"><span class="lbl">Quick wins you can do yourself</span><ul class="nl">`+qw+`</ul></div><div><span class="lbl">How we would fix the bigger problems</span>`+hw+`</div></div>`, ""))

	// ---- Sources
	srcs := first(d.Objects("sources"), 14)
	if len(srcs) > 0 {
		li := ""
		for _, s := range srcs {
			tl := s.S("title")
			if tl == "" {
				tl = s.S("publisher")
			}
			pub := s.S("publisher")
			if y := s.S("year"); y != "" {
				pub += ", " + y
			}
			li += `<li><b>` + t(tl, 90) + `</b><small>` + t(pub, 60) + `</small>`
			if u := s.S("url"); u != "" {
				li += `<a class="u" href="` + esc(safeURL(u)) + `" target="_blank" rel="noopener nofollow">` + esc(trunc(wwwRe.ReplaceAllString(u, ""), 80)) + `</a>`
			}
			li += `</li>`
		}
		what := l.Category
		if what == "" {
			what = "this business type"
		}
		if l.City != "" {
			what += " in " + l.City
		}
		o.WriteString(slide("Research", "Sources behind this report", "Market figures were researched for "+what+" on "+now.Format("02 Jan 2006")+".", `<ul class="srcs">`+li+`</ul>`, ""))
	}

	// ---- Closing
	svc := ""
	k := 0
	for _, s := range strings.Split(S("services", ""), "\n") {
		if s = strings.TrimSpace(s); s != "" && k < 5 {
			svc += `<li>` + t(s, 60) + `</li>`
			k++
		}
	}
	basis := "Market figures come from the sources named"
	if len(d.L("sources")) == 0 {
		basis = "Market figures are general industry estimates"
	}
	art := ""
	if in.Art != nil {
		art = in.Art("cta")
	}
	if art == "" {
		art = illusURI("cta", P, A)
	}
	o.WriteString(`<section class="slide close"><div class="cl-l"><span class="kick">Thank you</span><h2>Let's fix what is holding you back</h2><p class="big-p">` + t(d.S("closing"), 280) + `</p><p>` + t(S("company_about", ""), 330) + `</p>` +
		`<div class="card soft"><span class="lbl">What we do</span><ul class="ticks">` + svc + `</ul></div><small class="note">Prepared on ` + now.Format("02 Jan 2006") + ` from live checks of the public website and Google listing. ` + basis + `; projections are conservative estimates.</small></div>` +
		`<div class="cl-r"><img class="cl-art" src="` + esc(art) + `" alt=""><h3>Free 20-minute walkthrough</h3><p>We will go through this report with you – no obligation.</p><div class="ct">`)
	if hasLogo {
		o.WriteString(logoImg("#FFFFFF", "", ""))
	}
	o.WriteString(`<b>` + esc(co) + `</b><span>` + esc(S("company_phone", "")) + `</span><span>` + esc(S("company_email", "")) + `</span><span>` + esc(web) + `</span></div></div></section>`)

	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>` + esc(l.Name) + ` – Growth audit</title><style>` + reportStyles(in, P, A) + `</style></head><body>` + o.String() + `</body></html>`
}

var wwwRe = regexp.MustCompile(`(?i)^https?://(www\.)?`)

// lowCats: category scores below 80, lowest first.
func lowCats(a J, hasWeb bool, n int) []kv {
	var out []kv
	m := catScores(a, hasWeb)
	for _, k := range sortedKeys(m) {
		if m[k] < 80 {
			out = append(out, kv{k, m[k]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].V < out[j].V })
	return out[:min(n, len(out))]
}

// reportStyles: font faces (built-in Inter / Inter Tight + uploaded brand fonts) and the slide CSS.
func reportStyles(in *RenderIn, P, A string) string {
	var b strings.Builder
	for _, f := range builtinFonts {
		u := f.File
		if in.FontURL != nil {
			u = in.FontURL(f.File)
		}
		fmt.Fprintf(&b, "@font-face{font-family:'%s';font-weight:%d;src:url('%s') format('truetype')}", f.Family, f.Weight, u)
	}
	css := strings.NewReplacer("$P", P, "$A", A).Replace(reportCSS)
	head, body := fontName(in.S.Get("font_heading", "")), fontName(in.S.Get("font_body", ""))
	for _, f := range []string{head, body} {
		if u, ok := in.CustomFonts[f]; ok && !stdFonts[f] {
			fmt.Fprintf(&b, "@font-face{font-family:'%s';src:url('%s') format('truetype')}", f, u)
		}
	}
	if _, ok := in.CustomFonts[head]; ok && !stdFonts[head] {
		css = strings.ReplaceAll(css, "'Inter Tight'", "'"+head+"','Inter Tight'")
	}
	if _, ok := in.CustomFonts[body]; ok && !stdFonts[body] {
		css = strings.Replace(css, "font:15px/1.55 Inter,", "font:15px/1.55 '"+body+"',Inter,", 1)
	}
	b.WriteString(css)
	return b.String()
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func first[T any](s []T, n int) []T { return s[:min(n, len(s))] }

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
