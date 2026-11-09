package reports

// Public web report /r/{token}, PDF /r/{token}.pdf (v1 report.php) and the report fonts.

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/secure"
)

var tokenRe = regexp.MustCompile(`^[a-f0-9]{16,64}$`)

// botRe: views by bots and link previews are not counted (v1).
var botRe = regexp.MustCompile(`(?i)bot|crawl|spider|facebookexternalhit|WhatsApp|preview|curl|wget|python|Headless`)

func nowIn(tz *time.Location) time.Time { return time.Now().In(tz) }

func publicFont(c *app.Ctx) (any, error) {
	b, ok := builtinFont(c.Param("name"))
	if !ok {
		return nil, app.NotFound()
	}
	c.Raw()
	h := c.W.Header()
	h.Set("Content-Type", "font/ttf")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("Access-Control-Allow-Origin", "*")
	c.W.Write(b)
	return nil, nil
}

// isStaff: the visitor is signed in to this workspace (team views are not counted, and the team
// can always get the PDF).
func isStaff(c *app.Ctx) bool {
	ck, err := c.R.Cookie(app.SessionCookie)
	if err != nil || ck.Value == "" || c.Org == nil {
		return false
	}
	n, _ := db.Val[int](c, c.A.DB.Pool, `SELECT 1 FROM sessions WHERE id=$1 AND org_id=$2 AND expires_at>now()`, secure.SHA256(ck.Value), c.Org.ID)
	return n == 1
}

func has(c *app.Ctx, k string) bool { _, ok := c.R.URL.Query()[k]; return ok }

func notFound(c *app.Ctx, msg string) (any, error) {
	c.Raw()
	c.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.W.WriteHeader(http.StatusNotFound)
	c.W.Write([]byte(msg))
	return nil, nil
}

func publicReport(c *app.Ctx) (any, error) {
	tok := c.Param("token")
	pdfURL := strings.HasSuffix(tok, ".pdf")
	tok = strings.TrimSuffix(tok, ".pdf")
	if !tokenRe.MatchString(tok) {
		return notFound(c, "Report not found")
	}
	rid, err := c.UseToken("report", tok)
	if err != nil {
		return notFound(c, "Report not found")
	}
	r, err := db.One[reportRow](c, c.Tx(), `SELECT `+reportCols+` FROM reports WHERE id=$1`, rid)
	if err != nil {
		return notFound(c, "Report not found")
	}
	c.W.Header().Set("X-Robots-Tag", "noindex")
	s := c.S()
	if has(c, "proposal") {
		f := localFile(c.A, c.Org.ID, s.Get("proposal_pdf", ""))
		if f == "" {
			return notFound(c, "Not found")
		}
		return servePDF(c, f, "Website-Proposal.pdf", false)
	}
	l, err := shared.GetLead(c, c.Tx(), r.LeadID)
	if err != nil {
		return notFound(c, "Report not available")
	}
	staff := isStaff(c)
	mode := reportMode(s)
	wantPDF := pdfURL || has(c, "dl") || has(c, "pdf") || (mode == "pdf" && !has(c, "web"))
	var pdfFile, pdfFileName string
	if wantPDF && (mode != "link" || staff) {
		// Rendered in its own transaction before this request touches the report row
		if pdfFile, pdfFileName, err = ensurePDF(c, c.A, c.Org.ID, r.ID); err != nil {
			if errors.Is(err, db.ErrNoRows) {
				return notFound(c, "Report not available")
			}
			return nil, err
		}
	}
	// Count only real external views (not your team, bots or link previews)
	if !staff && !botRe.MatchString(c.R.UserAgent()) {
		if _, err := c.Tx().Exec(c, `UPDATE reports SET views=views+1 WHERE id=$1`, r.ID); err != nil {
			return nil, err
		}
		if r.Views == 0 {
			if err := app.Event(c, c.Tx(), r.LeadID, "viewed", "Report opened by the business", "web", nil); err != nil {
				return nil, err
			}
			if l.Stage == "contacted" || l.Stage == "report_ready" {
				if _, err := c.Tx().Exec(c, `UPDATE leads SET stage='opened' WHERE id=$1`, r.LeadID); err != nil {
					return nil, err
				}
			}
		}
	}
	if pdfFile != "" {
		return servePDF(c, pdfFile, pdfFileName, has(c, "dl"))
	}
	// Online report: same slides as the PDF, drawn by the visitor's browser and scaled to the screen
	in, err := renderInput(c, c.A, c.Tx(), s, c.Org.ID, &r, func(f string) string { return "/r/fonts/" + f })
	if err != nil {
		return nil, err
	}
	// Reports made before link checking: check their sources once and store the result
	if len(in.Data.L("sources")) > 0 && in.Data["_src"] == nil {
		in.Data = verifySources(c, nil, in.Data)
		if b, err := json.Marshal(in.Data); err == nil {
			c.Tx().Exec(c, `UPDATE reports SET html=$2 WHERE id=$1`, r.ID, string(b))
		}
	}
	page, err := webPage(c, in, l, tok, mode, staff)
	if err != nil {
		return nil, err
	}
	c.Raw()
	c.W.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.W.Header().Set("Cache-Control", "no-store")
	c.W.Write([]byte(page))
	return nil, nil
}

var barTpl = template.Must(template.New("bar").Parse(`<div class="wb"><div class="wb-l">{{.Logo}}<span><b>{{.Company}}</b><small>Growth audit for {{.Lead}}</small></span></div><div class="wb-r">` +
	`{{if .PDF}}<a class="wbb" href="{{.PDF}}">Download PDF</a>{{end}}` +
	`{{if .WA}}<a class="wbb" href="{{.WA}}" target="_blank" rel="noopener">WhatsApp us</a>{{end}}` +
	`{{if .Book}}<a class="wbb pri" href="{{.Book}}">{{.CTA}}</a>{{end}}</div></div>`))

// On screen the report is one continuous page (no page breaks, nothing clipped); printing still gives A4 slides.
const webCSS = `<style>@media screen{html{background:#E9ECF2}body{padding:84px 0 40px}.rw{width:297mm;margin:0 auto;background:#fff;border-radius:14px;box-shadow:0 10px 40px rgba(15,23,42,.14);overflow:hidden;zoom:var(--z,1)}.rw .slide{width:auto;height:auto!important;min-height:0;overflow:visible!important;page-break-after:auto;break-after:auto;padding-bottom:14mm}.rw .slide+.slide{border-top:1px solid #E6EAF2}.rw .slide:not(.cover):not(.close) .sb{flex:none}.rw .cover,.rw .close{min-height:190mm;padding:0!important}.rw .cover{border-radius:0}.rw *{-webkit-line-clamp:unset!important}.rw .pno{display:none}.wb{position:fixed;top:0;left:0;right:0;z-index:9;height:64px;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:0 20px;background:#0F1F45;color:#fff;font-family:Inter,system-ui,sans-serif}.wb-l{display:flex;align-items:center;gap:12px;min-width:0}.wb-l img{height:38px;max-width:120px;object-fit:contain}.wb-l b{display:block;font-size:14px}.wb-l small{display:block;font-size:12px;color:#A9B6D6;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.wb-r{display:flex;gap:8px;flex:none}.wbb{display:inline-flex;align-items:center;height:38px;padding:0 14px;border-radius:10px;border:1px solid rgba(255,255,255,.25);color:#fff;text-decoration:none;font-size:13.5px;font-weight:600}.wbb.pri{background:#2563EB;border-color:#2563EB}@media(max-width:700px){.wb{height:auto;padding:10px 12px;flex-wrap:wrap}.wbb{height:34px;font-size:12.5px;padding:0 10px}body{padding-top:120px}}}@media print{.wb{display:none}}</style>`

const webJS = `<script>(function(){var r=document.querySelector(".rw");function f(){r.style.setProperty("--z",Math.min(1,(document.documentElement.clientWidth-24)/1123))}f();addEventListener("resize",f)})()</script>`

// webPage wraps the report slides with the top bar: Download PDF, WhatsApp us and Book a call.
func webPage(c *app.Ctx, in *RenderIn, l *shared.Lead, tok, mode string, staff bool) (string, error) {
	s := c.S()
	doc := Render(in)
	book := shared.BookingLink(c.A, l)
	if book == "" {
		book = shared.GeneralBookingLink(c.A, s)
	}
	wa := ""
	if ph := onlyDigits(s.Get("company_phone", "")); ph != "" {
		if len(ph) == 10 {
			ph = onlyDigits(s.Get("default_country_code", "91")) + ph
		}
		wa = "https://wa.me/" + ph + "?text=" + url.QueryEscape("Hi, I read the growth audit for "+l.Name+".")
	}
	logo := ""
	if in.Logo != nil {
		if u, chip := in.Logo("#0F1F45"); u != "" {
			logo = `<img src="` + esc(u) + `" alt="">`
			if chip != "" {
				logo = `<span style="display:inline-block;background:` + chip + `;border-radius:10px;padding:7px 10px;line-height:0">` + logo + `</span>`
			}
		}
	}
	pdf := ""
	if mode != "link" || staff {
		pdf = "/r/" + tok + ".pdf?dl=1"
	}
	var bar bytes.Buffer
	if err := barTpl.Execute(&bar, map[string]any{"Logo": template.HTML(logo), "Company": s.Get("company_name", ""), "Lead": l.Name, "PDF": pdf,
		"WA": template.URL(wa), "Book": template.URL(safeURL(book)), "CTA": s.Get("report_cta", "Book a free strategy call")}); err != nil {
		return "", err
	}
	if book == "" {
		// no booking page: keep the bar without the button
		bar.Reset()
		barTpl.Execute(&bar, map[string]any{"Logo": template.HTML(logo), "Company": s.Get("company_name", ""), "Lead": l.Name, "PDF": pdf, "WA": template.URL(wa)})
	}
	doc = strings.Replace(doc, "</head>", webCSS+`<meta name="viewport" content="width=device-width,initial-scale=1"></head>`, 1)
	doc = strings.Replace(doc, "<body>", "<body>"+bar.String()+`<main class="rw">`, 1)
	doc = strings.Replace(doc, "</body>", "</main>"+webJS+"</body>", 1)
	return doc, nil
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
