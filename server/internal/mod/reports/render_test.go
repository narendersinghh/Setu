package reports

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/config"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

type fakeS map[string]string

func (f fakeS) Get(k, def string) string {
	if v, ok := f[k]; ok && v != "" {
		return v
	}
	return def
}

const sampleAudit = `{"website":{"score":62,"cats":{"Speed":48,"On-page SEO":71,"Technical":90},"meta":{},"checks":[
{"label":"Google Analytics","status":"fail","detail":"Not installed","w":3,"cat":"Technical"},
{"label":"Page speed","status":"warn","detail":"HTML took 2.9 s","w":2,"cat":"Speed"},
{"label":"HTTPS","status":"pass","detail":"Secure","w":2,"cat":"Technical"}]},
"map":{"score":71,"cats":{"Profile":64,"Reviews":82},"meta":{},"checks":[
{"label":"Photos","status":"fail","detail":"Only 3 photos <b>","w":3,"cat":"Profile"},
{"label":"Opening hours","status":"warn","detail":"Missing","w":2,"cat":"Profile"},
{"label":"Rating","status":"pass","detail":"4.6","w":2,"cat":"Reviews"}]}}`

func sampleIn(t *testing.T) *RenderIn {
	var au map[string]any
	if err := json.Unmarshal([]byte(sampleAudit), &au); err != nil {
		t.Fatal(err)
	}
	r1, r2 := 4.8, 4.2
	v1, v2 := 210, 35
	return &RenderIn{
		Lead: sampleLead(), Keyword: "dentist", Audit: J(au), Data: sampleData(), HasWeb: true,
		Comp: []Competitor{{Name: "Smile Dental", Rating: &r1, Reviews: &v1, Position: 1, Website: "https://smile.example"}, {Name: "Tooth & Co", Rating: &r2, Reviews: &v2, Position: 2}},
		S: fakeS{"brand_primary": "#0F1F45", "brand_accent": "#E8A317", "company_name": "Acme Growth", "company_phone": "+91 98765 43210", "company_website": "https://acme.example/",
			"company_email": "hello@acme.example", "company_about": "We help local businesses grow.", "services": "Local SEO\nGoogle Business Profile\nWebsites", "yearly_fee": "72000", "currency_symbol": "₹"},
		Now: time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC),
	}
}

func TestRenderSlides(t *testing.T) {
	in := sampleIn(t)
	h := Render(in)
	for _, want := range []string{
		"<title>Sample Business – Growth audit</title>", "Growth audit · November 2026", "Executive summary", "Problem map", "Market research",
		"Competition", "Smile Dental", "Detailed findings", "Checklist", "Your potential", "Roadmap", "Next steps", "Sources behind this report",
		"Let's fix what is holding you back", "Acme Growth", "--p:#0F1F45", "@font-face{font-family:'Inter Tight';font-weight:800;src:url('InterTight-ExtraBold.ttf')",
		"break-even", "Only 3 photos &lt;b&gt;",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("report HTML misses %q", want)
		}
	}
	// 12 missed enquiries × 12 months × 20% × 60,000
	if !strings.Contains(h, "₹17,28,000") {
		t.Errorf("revenue at stake not computed")
	}
	if strings.Contains(h, "<b>Only 3 photos <b>") || strings.Contains(h, "<script") {
		t.Errorf("unescaped content")
	}
	if n := strings.Count(h, `<section class="slide`); n < 12 {
		t.Errorf("expected at least 12 slides, got %d", n)
	}
	if out := os.Getenv("REPORT_SAMPLE_OUT"); out != "" {
		os.WriteFile(out, []byte(h), 0o644)
	}
}

func TestRenderEmptyData(t *testing.T) {
	in := sampleIn(t)
	in.Data, in.Audit, in.Comp, in.HasWeb = J{}, nil, nil, false
	h := Render(in)
	if !strings.Contains(h, "Google Maps audit &amp; website plan") || strings.Contains(h, "Competition") {
		t.Errorf("empty report not rendered as expected")
	}
}

func TestCustomFonts(t *testing.T) {
	in := sampleIn(t)
	in.S.(fakeS)["font_heading"] = "Brand-Head"
	in.CustomFonts = map[string]string{"Brand-Head": "data:font/ttf;base64,AAAA"}
	h := Render(in)
	if !strings.Contains(h, "font-family:'Brand-Head';src:url('data:font/ttf;base64,AAAA')") || !strings.Contains(h, "'Brand-Head','Inter Tight'") {
		t.Errorf("custom heading font not applied")
	}
}

func TestParseReport(t *testing.T) {
	d := parseReport("Here you go:\n```json\n{\"summary\":\"x\",\"findings\":[{\"title\":\"a\",},],}\n```", "summary")
	if d == nil || d.S("summary") != "x" || len(d.Objects("findings")) != 1 {
		t.Fatalf("parse failed: %#v", d)
	}
	if parseReport("no json", "summary") != nil || parseReport(`{"facts":[]}`, "facts") != nil {
		t.Fatal("expected nil")
	}
}

func TestHelpers(t *testing.T) {
	if num("1,900") != 1900 || num("₹60,000.50") != 60000.5 || num(nil) != 0 {
		t.Error("num")
	}
	if tint("#000000", .5) != "#808080" || !isDark("#0B0B0B") || isDark("#FFFFFF") {
		t.Error("colours")
	}
	if cleanGlyphs("Great ✅ job 🚀  now") != "Great ✓ job now" {
		t.Errorf("glyphs: %q", cleanGlyphs("Great ✅ job 🚀  now"))
	}
	if trunc("abcdef", 4) != "abc…" {
		t.Error("trunc")
	}
	if !strings.HasPrefix(illusSVG("cta", "#112233", "#445566"), "<svg") || strings.Contains(illusSVG("cta", "#112233", "#445566"), "$") {
		t.Error("illustration tokens not replaced")
	}
	if !strings.HasPrefix(chartGauge(42, "Web", 120), "data:image/svg+xml;base64,") {
		t.Error("gauge")
	}
	if pdfName("Joe's Café & Bar") != "Audit-Report-Joe-s-Caf-Bar.pdf" {
		t.Errorf("pdf name %q", pdfName("Joe's Café & Bar"))
	}
}

func TestVerifySources(t *testing.T) {
	old := linkCheck
	defer func() { linkCheck = old }()
	linkCheck = func(_ context.Context, u string) bool { return !strings.Contains(u, "broken") }
	d := J{"sources": []any{
		map[string]any{"title": "ok", "url": "https://ok.example/a"},
		map[string]any{"title": "bad", "url": "https://broken.example/"},
		map[string]any{"title": "no url"},
		map[string]any{"title": "ftp", "url": "ftp://x"},
	}}
	d = verifySources(context.Background(), nil, d)
	if l := d.Objects("sources"); len(l) != 1 || l[0].S("title") != "ok" || d["_src"] != 1 {
		t.Fatalf("sources: %#v", d["sources"])
	}
}

func TestWriteRequest(t *testing.T) {
	in := &aiInput{prompt: &promptRow{Content: "Review {{business_name}} in {{city}}"}, vars: map[string]string{"business_name": "Acme", "city": "Pune"}}
	u := writeRequest(in, J{"facts": []any{"x"}}, true)
	if !strings.HasPrefix(u, "Review Acme in Pune") || !strings.Contains(u, "RESEARCH BRIEF") {
		t.Errorf("request: %s", u)
	}
	if u := writeRequest(in, nil, false); !strings.Contains(u, "market research is switched OFF") {
		t.Errorf("research-off note missing")
	}
}

// Gotenberg is faked: the report HTML and the font assets must arrive as multipart files.
func TestRenderPDFWithFakeGotenberg(t *testing.T) {
	var got []string
	var html string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/chromium/convert/html" {
			http.NotFound(w, r)
			return
		}
		_, p, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, p["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if part.FileName() != "" {
				got = append(got, part.FileName())
				if part.FileName() == "index.html" {
					b, _ := io.ReadAll(part)
					html = string(b)
				}
			}
		}
		w.Write([]byte("%PDF-1.7 fake"))
	}))
	defer srv.Close()
	a := &app.App{Cfg: &config.Config{GotenbergURL: srv.URL}}
	b, err := renderPDF(context.Background(), a, sampleIn(t))
	if err != nil || !strings.HasPrefix(string(b), "%PDF") {
		t.Fatalf("pdf: %v %q", err, b)
	}
	if len(got) != 1+len(builtinFonts) || got[0] != "index.html" || !strings.Contains(html, "Sample Business") {
		t.Fatalf("files sent: %v", got)
	}
}

func TestPromptsAndTypes(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range defaultPrompts {
		seen[d[0]] = true
		if !typeOK(d[0]) || d[3] == "" {
			t.Errorf("bad default prompt %v", d[0])
		}
	}
	for _, ty := range PromptTypes {
		if !seen[ty[0]] {
			t.Errorf("no default for %s", ty[0])
		}
	}
	vars := map[string]string{"business_name": "Acme"}
	if !strings.Contains(shared.Tpl(defaultPrompts[4][2], vars), "Acme") {
		t.Error("email subject template")
	}
}
