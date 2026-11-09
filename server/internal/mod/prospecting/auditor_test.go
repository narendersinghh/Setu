package prospecting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/narendersinghh/setu/server/internal/netx"
)

const homePage = `<!doctype html><html lang="en"><head><title>Best Dentist in Pune | Smile Dental Clinic</title>
<meta name="viewport" content="width=device-width"><meta name="description" content="Smile Dental Clinic in Pune offers painless root canal, implants and braces. Book an appointment with our expert dentists today.">
<meta property="og:title" content="Smile"><meta property="og:image" content="/og.png">
<link rel="canonical" href="/"><link rel="icon" href="/favicon.ico"><link rel="stylesheet" href="/a.css">
<script src="/blocking.js"></script><script async src="https://www.googletagmanager.com/gtag/js?id=G-ABCDEF1"></script>
<script type="application/ld+json">{"@type":"Dentist","name":"Smile"}</script></head>
<body><h1>Smile Dental Clinic Pune</h1><h2>Services</h2><h2>Reviews</h2><h3>Implants</h3>
<p>Call us on <a href="tel:+919876543210">+91 98765 43210</a> or <a href="https://wa.me/919876543210">WhatsApp</a>. Email <a href="mailto:hello@smile.example.org">hello@smile.example.org</a> – info@smiledental.in</p>
<p>What our patients say: great clinic in Pune. Book an appointment now.</p>
<img src="/big.jpg" alt="clinic"><img src="/small.webp" alt=""><img data-src="/lazy.png" alt="x">
<a href="/services/implants/">Implants</a><a href="/services/braces/">Braces</a><a href="/about">About us</a><a href="/privacy">Privacy policy</a><a href="/broken">Old page</a><a href="/blog/">Blog</a>
<a href="https://www.facebook.com/smiledental">fb</a><a href="https://instagram.com/smile">ig</a><a href="https://www.facebook.com/sharer.php?u=x">share</a>
<a href="https://maps.google.com/?cid=1">Map</a><form><input name="n"></form>
<footer>© 2015 Smile</footer></body></html>`

func testSite(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, homePage)
	})
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "User-agent: *\nSitemap: /sitemap.xml")
	})
	mux.HandleFunc("/big.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "400000")
		if r.Method != "HEAD" {
			w.Write(make([]byte, 400000))
		}
	})
	mux.HandleFunc("/small.webp", func(w http.ResponseWriter, r *http.Request) { w.Write(make([]byte, 20000)) })
	for _, p := range []string{"/services/implants/", "/services/braces/", "/blog/"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `<html><head><title>Best Dentist in Pune | Smile Dental Clinic</title></head><body>x</body></html>`)
		})
	}
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>About</title><meta name="description" content="x"></head></html>`)
	})
	mux.HandleFunc("/privacy", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `<title>Privacy</title>`) })
	mux.HandleFunc("/rdap/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"events":[{"eventAction":"registration","eventDate":"2015-03-01T00:00:00Z"}]}`)
	})
	return httptest.NewServer(mux)
}

func useLocal(t *testing.T, srv *httptest.Server) {
	ow, oa, os, or := webDo, apiDo, sslDays, rdapURL
	webDo = func(ctx context.Context, r netx.Req) (*netx.Resp, error) { r.Public = false; return netx.Do(ctx, r) }
	sslDays = func(context.Context, string) (int, bool) { return 45, true }
	rdapURL = srv.URL + "/rdap/"
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("NO_PROXY", "*")
	t.Cleanup(func() { webDo, apiDo, sslDays, rdapURL = ow, oa, os, or })
}

type mapS map[string]string

func (m mapS) Get(k, def string) string {
	if v := m[k]; v != "" {
		return v
	}
	return def
}

func TestScanContacts(t *testing.T) {
	srv := testSite(t)
	defer srv.Close()
	useLocal(t, srv)
	c := scanContacts(context.Background(), srv.URL)
	if len(c.Phones) == 0 || c.Phones[0] != "+919876543210" {
		t.Fatalf("phones %v", c.Phones)
	}
	if c.WhatsApp != "919876543210" {
		t.Fatalf("whatsapp %q", c.WhatsApp)
	}
	want := map[string]bool{"info@smiledental.in": true}
	for _, e := range c.Emails {
		if strings.Contains(e, "example.") {
			t.Fatalf("example email kept: %v", c.Emails)
		}
		delete(want, e)
	}
	if len(want) > 0 {
		t.Fatalf("emails %v", c.Emails)
	}
	if c.Socials["facebook"] != "https://www.facebook.com/smiledental" || c.Socials["instagram"] == "" {
		t.Fatalf("socials %v", c.Socials)
	}
}

func TestPhoneFromText(t *testing.T) {
	var c Contacts
	extract(`<p>Call 020 2567 8901 today. Since 20200101 – ref 1234/5678901234</p>`, &c)
	if len(c.Phones) != 1 || c.Phones[0] != "020 2567 8901" {
		t.Fatalf("phones %v", c.Phones)
	}
}

func TestAuditWebsite(t *testing.T) {
	srv := testSite(t)
	defer srv.Close()
	useLocal(t, srv)
	s := mapS{"deep_crawl": "1"}
	var steps []string
	res, err := auditWebsite(context.Background(), s, srv.URL, func(p int, m string) error { steps = append(steps, m); return nil },
		AuditLead{City: "Pune", Keyword: "dentist", Phone: "+91 98765 43210"})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Check{}
	for _, c := range res.Checks {
		by[c.Label] = c
	}
	expect := map[string]string{
		"Secure HTTPS (SSL)": "fail", "Mobile friendly (viewport)": "pass", "Canonical tag": "pass", "Language declared": "pass", "Favicon": "pass",
		"robots.txt": "pass", "XML sitemap": "pass", "SEO title tag": "pass", "Meta description": "pass", "H1 heading": "pass",
		"Heading structure (H2/H3)": "pass", "City in title / main heading": "pass", "Main service keyword in title/description": "pass",
		"Local business schema markup": "pass", "Analytics / tracking": "pass", "Click-to-call button": "pass", "WhatsApp chat button": "pass",
		"Enquiry / contact form": "pass", "Clear call-to-action": "pass", "Google Maps embed / link": "pass", "Phone matches Google listing (NAP)": "pass",
		"Privacy policy / terms": "pass", "Testimonials / reviews on site": "pass", "About page": "pass", "Social media links": "pass",
		"Social sharing preview (Open Graph)": "pass", "Website freshness": "warn", "Broken internal links": "fail", "Unique page titles": "warn",
		"Dedicated service pages": "warn", "Blog / fresh content": "pass", "Image optimisation": "warn", "Render-blocking files": "pass",
		"Domain age": "pass", "Ads tracking pixel (Meta)": "warn", "Content depth (homepage)": "fail", "Image alt text": "fail",
	}
	for k, v := range expect {
		c, ok := by[k]
		if !ok {
			t.Errorf("missing check %q", k)
			continue
		}
		if c.Status != v {
			t.Errorf("%s = %s (%s), want %s", k, c.Status, c.Detail, v)
		}
	}
	if res.Score <= 0 || res.Score >= 100 || len(res.Cats) != 8 {
		t.Fatalf("score %d cats %v", res.Score, res.Cats)
	}
	b, _ := json.Marshal(res.Cats)
	if !strings.HasPrefix(string(b), `{"Technical":`) {
		t.Fatalf("cats order %s", b)
	}
	if len(res.Checks) < 38 {
		t.Fatalf("only %d checks", len(res.Checks))
	}
	t.Logf("score %d, %d checks, cats %s, steps %v", res.Score, len(res.Checks), b, steps)
}

func TestAuditDown(t *testing.T) {
	srv := testSite(t)
	defer srv.Close()
	useLocal(t, srv)
	res, err := auditWebsite(context.Background(), mapS{}, srv.URL+"/nope", func(int, string) error { return nil }, AuditLead{})
	if err != nil || res.Score != 0 || res.Meta["down"] != true {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestAuditMap(t *testing.T) {
	r := auditMap(AuditLead{Rating: 4.2, Reviews: 40, Position: 5, Keyword: "dentist", Website: "x", PhoneSource: "maps", Category: "Dentist",
		Raw: PlaceRaw{Photos: 3, Hours: []string{"Mon"}, Types: []string{"dentist", "health"}, Status: "OPERATIONAL"}},
		[]Competitor{{120, 4.8}, {80, 4.6}})
	if r.Score <= 0 || r.Meta["competitor_avg_reviews"] != 100 {
		t.Fatalf("%+v", r)
	}
	b, _ := json.Marshal(r.Cats)
	if string(b) != `{"Reputation":50,"Visibility":50,"Profile":79}` {
		t.Fatalf("cats %s", b)
	}
}

func TestBlockedPrivate(t *testing.T) {
	srv := testSite(t)
	defer srv.Close()
	// the real fetcher refuses loopback addresses (SSRF guard)
	if _, err := fetchSite(context.Background(), srv.URL); err == nil {
		t.Fatal("loopback fetch was not blocked")
	}
}

func TestAbsURL(t *testing.T) {
	for _, c := range [][3]string{
		{"/a", "https://x.com/b/c", "https://x.com/a"},
		{"d", "https://x.com/b/c", "https://x.com/b/d"},
		{"//cdn.x.com/i.png", "https://x.com/", "https://cdn.x.com/i.png"},
		{"mailto:a@b.c", "https://x.com/", ""},
		{"#top", "https://x.com/", ""},
		{"https://y.com/z", "https://x.com/", "https://y.com/z"},
	} {
		if got := absURL(c[0], c[1]); got != c[2] {
			t.Errorf("absURL(%q,%q)=%q want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestCSVCell(t *testing.T) {
	if csvCell("=SUM(A1)") != "'=SUM(A1)" || csvCell("-12") != "-12" || csvCell("+91 98") != "'+91 98" || csvCell("ok") != "ok" {
		t.Fatal("csv guard")
	}
}

// SETU_LIVE_AUDIT=https://… go test -run TestLiveAudit -v  (real website, real SSRF-guarded fetcher)
func TestLiveAudit(t *testing.T) {
	u := os.Getenv("SETU_LIVE_AUDIT")
	if u == "" {
		t.Skip("set SETU_LIVE_AUDIT")
	}
	res, err := auditWebsite(context.Background(), mapS{}, u, func(p int, m string) error { t.Logf("%d%% %s", p, m); return nil }, AuditLead{City: "London", Keyword: "python"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Checks {
		t.Logf("[%s] %-40s %s", c.Status, c.Label, c.Detail)
	}
	b, _ := json.Marshal(res.Meta)
	t.Logf("score %d meta %s", res.Score, b)
	c := scanContacts(context.Background(), u)
	t.Logf("contacts %+v", c)
}
