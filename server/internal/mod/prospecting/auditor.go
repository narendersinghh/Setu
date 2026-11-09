package prospecting

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/settings"
)

var (
	webCats = []string{"Technical", "Performance", "On-page SEO", "Content", "Conversion", "Local SEO", "Trust", "Social"}
	mapCats = []string{"Reputation", "Visibility", "Profile"}
)

// Check is one audit check (same JSON as v1).
type Check struct {
	Label  string `json:"label"`
	Status string `json:"status"` // pass | warn | fail
	Detail string `json:"detail"`
	W      int    `json:"w"`
	Cat    string `json:"cat"`
}

// Cats keeps the category scores in display order.
type Cats []CatScore

type CatScore struct {
	Cat   string
	Score int
}

func (c Cats) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, x := range c {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(x.Cat)
		b.Write(k)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(x.Score))
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (c *Cats) UnmarshalJSON(d []byte) error {
	var m map[string]int
	if err := json.Unmarshal(d, &m); err != nil {
		return nil
	}
	*c = nil
	for _, k := range append(append([]string{}, webCats...), mapCats...) {
		if v, ok := m[k]; ok {
			*c = append(*c, CatScore{k, v})
		}
	}
	return nil
}

// Result of a website or Google Maps audit.
type Result struct {
	Score  int            `json:"score"`
	Checks []Check        `json:"checks"`
	Meta   map[string]any `json:"meta"`
	Cats   Cats           `json:"cats"`
}

type checks []Check

func (a *checks) add(label, st, detail string, w int, cat string) {
	*a = append(*a, Check{Label: label, Status: st, Detail: detail, W: w, Cat: cat})
}

func score(cs []Check) int {
	t, g := 0.0, 0.0
	for _, c := range cs {
		t += float64(c.W)
		switch c.Status {
		case "pass":
			g += float64(c.W)
		case "warn":
			g += float64(c.W) * .5
		}
	}
	if t == 0 {
		return 0
	}
	return int(math.Round(g / t * 100))
}

func catScores(cs []Check, order []string) Cats {
	var out Cats
	for _, c := range order {
		var x []Check
		for _, k := range cs {
			if k.Cat == c {
				x = append(x, k)
			}
		}
		if len(x) > 0 {
			out = append(out, CatScore{c, score(x)})
		}
	}
	return out
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func tri(pass, warn bool) string {
	if pass {
		return "pass"
	}
	if warn {
		return "warn"
	}
	return "fail"
}

func rnd(f float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(f*p) / p
}

func fmtF(f float64, d int) string { return strconv.FormatFloat(rnd(f, d), 'f', -1, 64) }

func runes(s string) int { return utf8.RuneCountInString(s) }

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// strim = PHP mb_strimwidth(s, 0, n, '…')
func strim(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func ucwords(s string) string {
	f := strings.Fields(s)
	for i, w := range f {
		r := []rune(w)
		f[i] = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	return strings.Join(f, " ")
}

// AuditLead is what the audits need about a business.
type AuditLead struct {
	Name, City, Keyword, Category, Phone, PhoneSource, Website string
	Rating                                                     float64
	Reviews, Position                                          int
	Raw                                                        PlaceRaw
}

type progressFn func(pct int, msg string) error

var (
	mixedRe    = regexp.MustCompile(`(?i)(src|href)=["']http://`)
	anchorRe   = regexp.MustCompile(`(?i)<a\s[^>]*>`)
	noindexRe  = regexp.MustCompile(`(?i)<meta[^>]+name=["']robots["'][^>]+noindex`)
	modernRe   = regexp.MustCompile(`(?i)\.(webp|avif)(\?|$)`)
	ldRe       = regexp.MustCompile(`(?i)application/ld\+json`)
	lbRe       = regexp.MustCompile(`(?i)"@type"\s*:\s*"(LocalBusiness|Organization|[A-Za-z]*(Store|Business|Clinic|Restaurant|Dentist|Physician|Attorney|Hotel|Agency|School|Service))"`)
	blogRe     = regexp.MustCompile(`(?i)/(blog|news|articles|insights)`)
	svcRe      = regexp.MustCompile(`(?i)/(services?|treatments?|products?|solutions)/`)
	copyRe     = regexp.MustCompile(`(?i)(?:©|&copy;|copyright)\s*(?:\d{4}\s*[-–]\s*)?(\d{4})`)
	gaRe       = regexp.MustCompile(`googletagmanager\.com|google-analytics\.com|gtag\(|G-[A-Z0-9]{6,}`)
	pixelRe    = regexp.MustCompile(`connect\.facebook\.net|fbq\(`)
	telHrefRe  = regexp.MustCompile(`(?i)href=["']tel:`)
	waLinkRe   = regexp.MustCompile(`(?i)wa\.me/|api\.whatsapp\.com|whatsapp://`)
	ctaRe      = regexp.MustCompile(`(?i)\b(book (a|an|now|appointment)|get (a )?(free )?quote|call now|enquire now|contact us|schedule|request a call|get started)\b`)
	mapLinkRe  = regexp.MustCompile(`(?i)google\.com/maps|maps\.google|goo\.gl/maps|maps\.app\.goo\.gl`)
	privacyRe  = regexp.MustCompile(`privacy|terms`)
	testiRe    = regexp.MustCompile(`(?i)testimonial|what (our )?(clients|customers|patients) say|reviews?\b|google reviews|★★★`)
	aboutRe    = regexp.MustCompile(`(?i)/about`)
	crawlSkip  = regexp.MustCompile(`(?i)\.(pdf|jpe?g|png|zip)$|/cdn-cgi/|wp-login|/feed/?$|#`)
	metaDescRe = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["']`)
	titleRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	wordRe     = regexp.MustCompile(`[A-Za-z'-]+`)
	kwClean    = regexp.MustCompile(`[^a-z0-9 ]`)
)

// auditWebsite runs the ~45 website checks in 8 areas.
func auditWebsite(ctx context.Context, s *settings.Store, site string, P progressFn, l AuditLead) (*Result, error) {
	var ch checks
	m := map[string]any{}
	if err := P(5, "Opening the website"); err != nil {
		return nil, err
	}
	r, ferr := fetchSite(ctx, site)
	if ferr != nil || r.Code >= 400 || r.Code == 0 {
		why := ""
		if ferr != nil {
			why = ferr.Error()
		} else {
			why = "HTTP " + strconv.Itoa(r.Code)
		}
		ch.add("Website is reachable", "fail", "The website could not be opened ("+why+"). Visitors and Google see an error page.", 5, "Technical")
		return &Result{Score: 0, Checks: ch, Meta: map[string]any{"url": site, "down": true}, Cats: Cats{{"Technical", 0}}}, nil
	}
	h := string(r.Body)
	final := r.URL
	fu, _ := url.Parse(final)
	host := fu.Hostname()
	root := fu.Scheme + "://" + fu.Host
	m["url"] = final
	m["load_time"] = rnd(r.Duration.Seconds(), 2)
	m["ttfb"] = rnd(r.TTFB.Seconds(), 2)
	sizeKB := int(math.Round(float64(len(r.Body)) / 1024))
	m["size_kb"] = sizeKB
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	meta := func(n string) (string, bool) {
		v, ok := "", false
		doc.Find("meta").EachWithBreak(func(_ int, e *goquery.Selection) bool {
			nm, _ := e.Attr("name")
			pr, _ := e.Attr("property")
			if strings.ToLower(nm) == n || pr == n {
				if c, has := e.Attr("content"); has {
					v, ok = strings.TrimSpace(c), true
					return false
				}
			}
			return true
		})
		return v, ok
	}
	text := strings.TrimSpace(spaceRe.ReplaceAllString(pageText(h, true), " "))
	lt := strings.ToLower(text)
	city := strings.ToLower(strings.TrimSpace(l.City))
	kw := strings.ToLower(strings.TrimSpace(l.Keyword))
	if kw == "" {
		kw = strings.ToLower(strings.TrimSpace(l.Category))
	}

	// ---------- Technical
	if err := P(15, "Checking security & technical setup"); err != nil {
		return nil, err
	}
	https := strings.HasPrefix(strings.ToLower(final), "https://")
	ch.add("Secure HTTPS (SSL)", pick(https, "pass", "fail"), pick(https, "Site loads securely over HTTPS.", `Site is not secure (no HTTPS). Browsers show "Not secure" and Google ranks it lower.`), 3, "Technical")
	if https {
		if days, ok := sslDays(ctx, host); ok {
			m["ssl_days"] = days
			ch.add("SSL certificate validity", tri(days > 20, days > 0), pick(days > 0, "Certificate valid for "+strconv.Itoa(days)+" more days.", "Certificate has expired – visitors see a security warning."), 1, "Technical")
		}
		if hr, err := webDo(ctx, netx.Req{URL: "http://" + host + "/", Timeout: secs(10), Max: 50_000}); err == nil && hr.Code != 0 {
			ok := strings.HasPrefix(strings.ToLower(hr.URL), "https://")
			ch.add("HTTP → HTTPS redirect", pick(ok, "pass", "warn"), pick(ok, "Visitors typing http:// are moved to the secure site.", "The insecure http:// version does not redirect to https:// – duplicate site in Google."), 1, "Technical")
		}
	}
	if https && hasMixed(anchorRe.ReplaceAllString(h, "")) {
		ch.add("Mixed content", "warn", "Some images/scripts load over insecure http:// on a secure page – browsers may block them.", 1, "Technical")
	}
	vp := doc.Find(`meta[name="viewport"]`).Length() > 0
	ch.add("Mobile friendly (viewport)", pick(vp, "pass", "fail"), pick(vp, "Mobile viewport is set.", "Not mobile-optimised – most local searches happen on phones."), 3, "Technical")
	can := doc.Find(`link[rel="canonical"]`).Length() > 0
	ch.add("Canonical tag", pick(can, "pass", "warn"), pick(can, "Canonical URL set.", "No canonical tag – risk of duplicate-content issues."), 1, "Technical")
	lang := doc.Find("html[lang]").Length() > 0
	ch.add("Language declared", pick(lang, "pass", "warn"), pick(lang, "Page language is declared.", "No language attribute – search engines must guess the language."), 1, "Technical")
	fav := doc.Find(`link[rel*="icon"]`).Length() > 0
	ch.add("Favicon", pick(fav, "pass", "warn"), pick(fav, "Favicon present.", "No favicon – looks unprofessional in browser tabs and Google mobile results."), 1, "Technical")
	rb, rbErr := webDo(ctx, netx.Req{URL: root + "/robots.txt", Timeout: secs(10), Max: 200_000})
	hasRobots := rbErr == nil && rb.Code == 200 && !strings.Contains(strings.ToLower(rb.Header.Get("Content-Type")), "html")
	ch.add("robots.txt", pick(hasRobots, "pass", "warn"), pick(hasRobots, "robots.txt found.", "No robots.txt file."), 1, "Technical")
	hasSm := false
	if sm, err := webDo(ctx, netx.Req{URL: root + "/sitemap.xml", Timeout: secs(10), Max: 200_000}); err == nil && sm.Code == 200 {
		b := strings.ToLower(string(sm.Body))
		hasSm = strings.Contains(b, "<urlset") || strings.Contains(b, "<sitemapindex")
	}
	if !hasSm && hasRobots && strings.Contains(strings.ToLower(string(rb.Body)), "sitemap:") {
		hasSm = true
	}
	ch.add("XML sitemap", pick(hasSm, "pass", "fail"), pick(hasSm, "XML sitemap found.", "No XML sitemap – Google may miss pages."), 2, "Technical")
	if noindexRe.MatchString(h) {
		ch.add("Search engine indexing", "fail", `Page has a "noindex" tag – it is hidden from Google!`, 5, "Technical")
	}

	// ---------- Performance
	if err := P(30, "Measuring speed & page weight"); err != nil {
		return nil, err
	}
	sec := r.Duration.Seconds()
	ch.add("Page load speed", tri(sec < 2.5, sec < 5), "Homepage HTML loaded in "+fmtF(sec, 1)+"s (server response "+fmtF(r.TTFB.Seconds(), 2)+"s). "+pick(sec < 2.5, "Good.", "Slow pages lose visitors – over half leave after 3 seconds on mobile."), 3, "Performance")
	if sizeKB > 800 {
		ch.add("HTML page weight", pick(sizeKB > 2000, "fail", "warn"), "Homepage HTML alone is "+strconv.Itoa(sizeKB)+" KB – heavy for mobile data.", 1, "Performance")
	}
	block := 0
	doc.Find("head > script[src]").Each(func(_ int, e *goquery.Selection) {
		_, as := e.Attr("async")
		_, df := e.Attr("defer")
		if t, _ := e.Attr("type"); !as && !df && t != "module" {
			block++
		}
	})
	css := 0
	doc.Find("head > link").Each(func(_ int, e *goquery.Selection) {
		if rel, _ := e.Attr("rel"); rel == "stylesheet" {
			css++
		}
	})
	m["blocking"] = block + css
	ch.add("Render-blocking files", tri(block+css <= 4, block+css <= 10), fmt.Sprintf("%d scripts/stylesheets must load before anything shows (%d scripts, %d CSS files).", block+css, block, css), 2, "Performance")
	imgs := doc.Find("img")
	var srcs []string
	lazy, modern, noalt := 0, 0, 0
	imgs.Each(func(_ int, i *goquery.Selection) {
		s, _ := i.Attr("src")
		ds, _ := i.Attr("data-src")
		if s == "" {
			s = ds
		}
		if s != "" && !strings.HasPrefix(s, "data:") {
			if u := absURL(s, final); u != "" {
				srcs = append(srcs, u)
			}
		}
		if ld, _ := i.Attr("loading"); strings.ToLower(ld) == "lazy" || ds != "" {
			lazy++
		}
		if modernRe.MatchString(s) {
			modern++
		}
		if alt, _ := i.Attr("alt"); strings.TrimSpace(alt) == "" {
			noalt++
		}
	})
	srcs = uniq(srcs)
	m["images"] = len(srcs)
	if len(srcs) > 0 {
		sample := srcs
		if len(sample) > 8 {
			sample = sample[:8]
		}
		sizes := make([]int64, len(sample))
		var wg sync.WaitGroup
		for i, s := range sample {
			wg.Add(1)
			go func(i int, s string) { defer wg.Done(); sizes[i] = headSize(ctx, s) }(i, s)
		}
		wg.Wait()
		big, n := 0, 0
		var tot int64
		for _, hd := range sizes {
			if hd > 0 {
				n++
				tot += hd
				if hd > 300*1024 {
					big++
				}
			}
		}
		if n > 0 {
			avg := int(math.Round(float64(tot) / float64(n) / 1024))
			m["img_avg_kb"] = avg
			ch.add("Image optimisation", tri(big == 0, big <= 2), pick(big > 0, fmt.Sprintf("%d of %d sampled images are over 300 KB (average %d KB) – they slow the site down.", big, n, avg), fmt.Sprintf("Sampled images are well compressed (average %d KB).", avg)), 2, "Performance")
		}
		ch.add("Modern image formats (WebP/AVIF)", pick(float64(modern)/float64(len(srcs)) >= .5, "pass", "warn"), fmt.Sprintf("%d of %d images use modern formats. WebP is ~30%% smaller than JPG/PNG.", modern, len(srcs)), 1, "Performance")
		if len(srcs) > 5 {
			ch.add("Lazy-loading images", pick(float64(lazy)/float64(len(srcs)) >= .4, "pass", "warn"), fmt.Sprintf("%d of %d images load lazily – the rest all load at once.", lazy, len(srcs)), 1, "Performance")
		}
	}
	if s.Get("pagespeed", "0") == "1" {
		if err := P(38, "Running Google PageSpeed test (mobile)"); err != nil {
			return nil, err
		}
		if ps := pageSpeed(ctx, s, final); ps != nil {
			m["pagespeed"] = ps
			ch.add("Google PageSpeed (mobile)", tri(ps.Score >= 80, ps.Score >= 50), fmt.Sprintf("Google PageSpeed mobile score %d/100 · Largest content shows in %ss · Layout shift %s · Blocking time %dms.", ps.Score, fmtF(ps.LCP, 1), fmtF(ps.CLS, 2), ps.TBT), 3, "Performance")
		}
	}

	// ---------- On-page SEO
	if err := P(48, "Analysing on-page SEO"); err != nil {
		return nil, err
	}
	title := ""
	if t := doc.Find("title").First(); t.Length() > 0 {
		title = strings.TrimSpace(spaceRe.ReplaceAllString(t.Text(), " "))
	}
	m["title"] = title
	tl := runes(title)
	ch.add("SEO title tag", pick(title == "", "fail", pick(tl < 20 || tl > 65, "warn", "pass")), pick(title == "", "No page title – Google does not know what this page is about.", `"`+cut(title, 80)+`" (`+strconv.Itoa(tl)+" chars). "+pick(tl < 20 || tl > 65, "Ideal length is 30-60 characters with main service + city.", "Good length.")), 3, "On-page SEO")
	d, hasD := meta("description")
	if hasD {
		m["description"] = d
	} else {
		m["description"] = nil
	}
	dl := runes(d)
	ch.add("Meta description", pick(d == "", "fail", pick(dl < 70 || dl > 170, "warn", "pass")), pick(d == "", "Missing meta description – Google shows random text in search results, lowering clicks.", strconv.Itoa(dl)+" characters. "+pick(dl < 70 || dl > 170, "Ideal is 120-160 characters.", "Good.")), 2, "On-page SEO")
	h1 := doc.Find("h1")
	h1t := ""
	if h1.Length() > 0 {
		h1t = strings.TrimSpace(h1.First().Text())
	}
	m["h1"] = h1t
	h1d := `One H1: "` + cut(h1t, 70) + `"`
	if h1.Length() == 0 {
		h1d = "No H1 heading – the main topic of the page is unclear to Google."
	} else if h1.Length() > 1 {
		h1d = strconv.Itoa(h1.Length()) + " H1 headings found – use exactly one."
	}
	ch.add("H1 heading", pick(h1.Length() == 1, "pass", pick(h1.Length() == 0, "fail", "warn")), h1d, 2, "On-page SEO")
	h2, h3 := doc.Find("h2").Length(), doc.Find("h3").Length()
	ch.add("Heading structure (H2/H3)", pick(h2 >= 2, "pass", "warn"), fmt.Sprintf("%d H2 and %d H3 sub-headings found.", h2, h3), 1, "On-page SEO")
	if city != "" {
		inT := strings.Contains(strings.ToLower(title+" "+h1t), city)
		ch.add("City in title / main heading", pick(inT, "pass", "warn"), pick(inT, `Mentions "`+ucwords(city)+`" in the title or H1 – good for local searches.`, `"`+ucwords(city)+`" is not in the title or H1 – the page is weaker for "near me" and city searches.`), 2, "On-page SEO")
	}
	if kw != "" {
		var words []string
		for _, w := range strings.Split(kwClean.ReplaceAllString(kw, ""), " ") {
			if len(w) > 3 {
				words = append(words, w)
			}
		}
		hay := strings.ToLower(title + " " + h1t + " " + d)
		hit := 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				hit++
			}
		}
		if len(words) > 0 {
			ch.add("Main service keyword in title/description", pick(hit > 0, "pass", "warn"), pick(hit > 0, `The main service ("`+kw+`") appears in title/heading/description.`, `The search phrase "`+kw+`" does not appear in the title, H1 or description.`), 2, "On-page SEO")
		}
	}
	if n := imgs.Length(); n > 0 {
		ch.add("Image alt text", tri(noalt == 0, float64(noalt)/float64(n) < .3), fmt.Sprintf("%d of %d images have no alt text (missed image-search traffic & accessibility).", noalt, n), 1, "On-page SEO")
	}
	schema := ldRe.MatchString(h)
	lb := lbRe.MatchString(h)
	ch.add("Local business schema markup", tri(lb, schema), pick(lb, "LocalBusiness/Organization schema found.", pick(schema, "Some structured data, but no LocalBusiness schema.", "No structured data – Google cannot show hours, rating and address as rich results.")), 2, "Local SEO")

	// ---------- Content
	wc := len(wordRe.FindAllString(text, -1))
	m["words"] = wc
	ch.add("Content depth (homepage)", tri(wc >= 600, wc >= 300), fmt.Sprintf("About %d words on the homepage. ", wc)+pick(wc < 600, "Thin pages rank poorly – Google prefers 600+ genuinely helpful words.", "Good."), 2, "Content")
	type link struct{ u, t string }
	var links []link
	idx := map[string]int{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u := absURL(href, final)
		if u == "" || hostOf(u) != host {
			return
		}
		if i := strings.IndexByte(u, '#'); i >= 0 {
			u = u[:i]
		}
		t := strings.ToLower(strings.TrimSpace(a.Text()))
		if i, ok := idx[u]; ok {
			links[i].t = t
			return
		}
		idx[u] = len(links)
		links = append(links, link{u, t})
	})
	m["internal_links"] = len(links)
	blog, svc, privacy, about := false, 0, false, false
	for _, x := range links {
		blog = blog || blogRe.MatchString(x.u)
		if svcRe.MatchString(x.u) {
			svc++
		}
		privacy = privacy || privacyRe.MatchString(x.t)
		about = about || aboutRe.MatchString(x.u)
	}
	ch.add("Blog / fresh content", pick(blog, "pass", "warn"), pick(blog, "Has a blog/news section.", "No blog or articles – no fresh content for Google and no answers to customers' questions."), 1, "Content")
	ch.add("Dedicated service pages", tri(svc >= 3, svc > 0), pick(svc > 0, strconv.Itoa(svc)+" service pages linked.", "No separate service pages – each service needs its own page to rank for it."), 2, "Content")
	if cm := copyRe.FindAllStringSubmatch(h, -1); len(cm) > 0 {
		y := 0
		for _, x := range cm {
			if n, _ := strconv.Atoi(x[1]); n > y {
				y = n
			}
		}
		if y > 0 && y < time.Now().Year()-1 {
			ch.add("Website freshness", "warn", "Footer copyright says "+strconv.Itoa(y)+" – the site looks outdated to visitors.", 1, "Content")
		}
	}

	// ---------- Conversion
	if err := P(60, "Checking how the site turns visitors into enquiries"); err != nil {
		return nil, err
	}
	ga := gaRe.MatchString(h)
	ch.add("Analytics / tracking", pick(ga, "pass", "fail"), pick(ga, "Google Analytics/Tag Manager installed.", "No Google Analytics – the business cannot measure visitors or enquiries."), 2, "Conversion")
	px := pixelRe.MatchString(h)
	ch.add("Ads tracking pixel (Meta)", pick(px, "pass", "warn"), pick(px, "Meta pixel installed – can retarget visitors.", "No Meta pixel – visitors cannot be retargeted with ads."), 1, "Conversion")
	tel := telHrefRe.MatchString(h)
	ch.add("Click-to-call button", pick(tel, "pass", "fail"), pick(tel, "Phone number is clickable.", "Phone number is not clickable on mobile – callers have to copy it manually."), 2, "Conversion")
	wa := waLinkRe.MatchString(h)
	ch.add("WhatsApp chat button", pick(wa, "pass", "warn"), pick(wa, "WhatsApp link present.", "No WhatsApp chat button – a big missed channel for enquiries in India."), 1, "Conversion")
	form := doc.Find("form").Length() > 0
	ch.add("Enquiry / contact form", pick(form, "pass", "warn"), pick(form, "Enquiry form found.", "No enquiry form on the homepage."), 1, "Conversion")
	cta := ctaRe.MatchString(text)
	ch.add("Clear call-to-action", pick(cta, "pass", "warn"), pick(cta, "Has clear action buttons (book / call / quote).", `No clear "Book now / Get a quote / Call now" message – visitors don't know the next step.`), 2, "Conversion")

	// ---------- Local SEO
	mp := mapLinkRe.MatchString(h)
	ch.add("Google Maps embed / link", pick(mp, "pass", "warn"), pick(mp, "Website links to Google Maps.", "Website does not embed or link to the Google Maps listing (weak local signal)."), 1, "Local SEO")
	if city != "" {
		n := strings.Count(lt, city)
		ch.add("City / area mentioned on site", tri(n >= 2, n > 0), fmt.Sprintf(`"%s" appears %d time(s) on the homepage.`, ucwords(city), n), 2, "Local SEO")
	}
	if ph := digits(l.Phone); len(ph) >= 10 {
		ph = ph[len(ph)-10:]
		in := strings.Contains(digits(h), ph)
		ch.add("Phone matches Google listing (NAP)", pick(in, "pass", "warn"), pick(in, "Same phone number on website and Google – consistent business details.", "The phone number on Google Maps is not shown on the website – inconsistent details reduce local ranking trust."), 2, "Local SEO")
	}

	// ---------- Trust
	ch.add("Privacy policy / terms", pick(privacy, "pass", "warn"), pick(privacy, "Privacy policy or terms page linked.", "No privacy policy – required for ads and builds trust."), 1, "Trust")
	testi := testiRe.MatchString(text)
	ch.add("Testimonials / reviews on site", pick(testi, "pass", "warn"), pick(testi, "Shows testimonials or reviews.", "No customer testimonials on the homepage – social proof is the #1 trust signal."), 2, "Trust")
	ch.add("About page", pick(about, "pass", "warn"), pick(about, "About page found.", "No About page – customers want to know who they are dealing with."), 1, "Trust")
	if err := P(70, "Checking domain age"); err != nil {
		return nil, err
	}
	if age, ok := domainAge(ctx, host); ok {
		m["domain_age"] = age
		ch.add("Domain age", pick(age >= 2, "pass", "warn"), fmt.Sprintf("Domain registered about %d year(s) ago. ", age)+pick(age < 2, "Newer domains need extra effort to earn Google's trust.", "An established domain helps rankings."), 1, "Trust")
	}

	// ---------- Social
	var socials []string
	lh := strings.ToLower(h)
	for _, x := range []string{"facebook", "instagram", "linkedin", "youtube"} {
		if strings.Contains(lh, x+".com/") {
			socials = append(socials, x)
		}
	}
	ch.add("Social media links", tri(len(socials) >= 2, len(socials) > 0), pick(len(socials) > 0, "Links to: "+strings.Join(socials, ", ")+".", "No social media profiles linked."), 1, "Social")
	ogT, _ := meta("og:title")
	ogI, _ := meta("og:image")
	og := ogT != "" && ogI != ""
	ch.add("Social sharing preview (Open Graph)", pick(og, "pass", "warn"), pick(og, "Open Graph tags present.", "Missing Open Graph tags – links shared on WhatsApp/Facebook show no preview image."), 1, "Social")

	// ---------- Inner pages & broken links
	if s.Get("deep_crawl", "1") == "1" && len(links) > 0 {
		if err := P(78, "Crawling inner pages & checking for broken links"); err != nil {
			return nil, err
		}
		var pk []string
		for _, x := range links {
			if x.u == final || x.u == strings.TrimRight(final, "/") || crawlSkip.MatchString(x.u) {
				continue
			}
			pk = append(pk, x.u)
			if len(pk) == 10 {
				break
			}
		}
		type page struct {
			bad   bool
			body  string
			fetch bool
		}
		res := make([]page, len(pk))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for i, u := range pk {
			wg.Add(1)
			go func(i int, u string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				pr, err := webDo(ctx, netx.Req{URL: u, Timeout: secs(8), Max: 150_000})
				if err != nil || pr.Code >= 400 || pr.Code == 0 {
					res[i] = page{bad: true}
					return
				}
				res[i] = page{body: string(pr.Body), fetch: true}
			}(i, u)
		}
		wg.Wait()
		var bad []string
		noDesc, crawled := 0, 0
		titles := []string{title}
		for i, p := range res {
			if p.bad {
				bad = append(bad, pk[i])
				continue
			}
			if i < 5 {
				crawled++
				if !metaDescRe.MatchString(p.body) {
					noDesc++
				}
				if tm := titleRe.FindStringSubmatch(p.body); tm != nil {
					titles = append(titles, strings.TrimSpace(tm[1]))
				}
			}
		}
		m["crawled"] = crawled
		m["broken"] = len(bad)
		ch.add("Broken internal links", pick(len(bad) > 0, "fail", "pass"), pick(len(bad) > 0, fmt.Sprintf("%d of %d tested links are broken (e.g. %s) – frustrating for visitors and Google.", len(bad), len(pk), strim(firstOr(bad), 70)), fmt.Sprintf("All %d tested internal links work.", len(pk))), 2, "Technical")
		if crawled > 0 {
			ch.add("Inner pages: meta descriptions", pick(noDesc == 0, "pass", "warn"), fmt.Sprintf("%d of %d inner pages are missing a meta description.", noDesc, crawled), 1, "On-page SEO")
			seen := map[string]bool{}
			for _, t := range titles {
				seen[strings.ToLower(t)] = true
			}
			dup := len(titles) - len(seen)
			ch.add("Unique page titles", pick(dup > 0, "warn", "pass"), pick(dup > 0, fmt.Sprintf("%d pages share the same title – Google cannot tell them apart.", dup), "Each crawled page has its own title."), 1, "On-page SEO")
		}
	}
	if err := P(88, "Website audit complete"); err != nil {
		return nil, err
	}
	return &Result{Score: score(ch), Checks: ch, Meta: m, Cats: catScores(ch, webCats)}, nil
}

var w3Re = regexp.MustCompile(`(?i)(src|href)=["']http://www\.w3\.org`)

// hasMixed: an http:// src/href that is not an http://www.w3.org namespace (RE2 has no lookahead).
func hasMixed(h string) bool { return len(mixedRe.FindAllString(h, -1)) > len(w3Re.FindAllString(h, -1)) }

func firstOr(a []string) string {
	if len(a) == 0 {
		return ""
	}
	return a[0]
}

// sslDays: days until the certificate of host expires (only public addresses are contacted).
var sslDays = func(ctx context.Context, host string) (int, bool) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return 0, false
	}
	for _, ip := range ips {
		if !netx.IsPublic(ip) {
			return 0, false
		}
	}
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second}, Config: &tls.Config{ServerName: host, InsecureSkipVerify: true}}
	c, err := d.DialContext(ctx, "tcp", netip.AddrPortFrom(ips[0], 443).String())
	if err != nil {
		return 0, false
	}
	defer c.Close()
	certs := c.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return 0, false
	}
	return int(math.Floor(time.Until(certs[0].NotAfter).Hours() / 24)), true
}

// headSize: Content-Length of an image (HEAD request).
func headSize(ctx context.Context, u string) int64 {
	r, err := webDo(ctx, netx.Req{Method: "HEAD", URL: u, Timeout: secs(6), Max: 1})
	if err != nil || r.Code >= 400 {
		return 0
	}
	n, _ := strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
	return n
}

var rdapURL = "https://rdap.org/domain/"

// domainAge: years since the domain was registered (RDAP).
func domainAge(ctx context.Context, host string) (int, bool) {
	d := strings.TrimPrefix(host, "www.")
	r, err := webDo(ctx, netx.Req{URL: rdapURL + url.PathEscape(d), Timeout: secs(8), Headers: map[string]string{"Accept": "application/rdap+json"}})
	if err != nil || r.Code != 200 {
		return 0, false
	}
	var x struct {
		Events []struct {
			Action string `json:"eventAction"`
			Date   string `json:"eventDate"`
		} `json:"events"`
	}
	if r.JSON(&x) != nil {
		return 0, false
	}
	for _, e := range x.Events {
		if e.Action == "registration" {
			t, err := time.Parse(time.RFC3339, e.Date)
			if err != nil {
				return 0, false
			}
			return max(0, int(math.Floor(time.Since(t).Seconds()/31557600))), true
		}
	}
	return 0, false
}

type PageSpeed struct {
	Score int     `json:"score"`
	LCP   float64 `json:"lcp"`
	CLS   float64 `json:"cls"`
	TBT   int     `json:"tbt"`
}

var pagespeedURL = "https://www.googleapis.com/pagespeedonline/v5/runPagespeed"

// pageSpeedTrack is set by the job to record usage.
type pageSpeedCtxKey struct{}

func pageSpeed(ctx context.Context, s *settings.Store, u string) *PageSpeed {
	q := url.Values{"url": {u}, "strategy": {"mobile"}, "category": {"performance"}}
	if k := s.Get("google_api_key", ""); k != "" {
		q.Set("key", k)
	}
	r, err := apiDo(ctx, netx.Req{URL: pagespeedURL + "?" + q.Encode(), Timeout: secs(70)})
	if t, ok := ctx.Value(pageSpeedCtxKey{}).(usageFn); ok {
		t("pagespeed", "mobile", err != nil || r.Code != 200)
	}
	if err != nil || r.Code != 200 {
		return nil
	}
	var d struct {
		L *struct {
			Categories struct {
				Performance struct {
					Score float64 `json:"score"`
				} `json:"performance"`
			} `json:"categories"`
			Audits map[string]struct {
				NumericValue float64 `json:"numericValue"`
			} `json:"audits"`
		} `json:"lighthouseResult"`
	}
	if r.JSON(&d) != nil || d.L == nil {
		return nil
	}
	a := d.L.Audits
	return &PageSpeed{Score: int(math.Round(d.L.Categories.Performance.Score * 100)), LCP: rnd(a["largest-contentful-paint"].NumericValue/1000, 1), CLS: rnd(a["cumulative-layout-shift"].NumericValue, 2), TBT: int(a["total-blocking-time"].NumericValue)}
}

// Competitor rating/reviews used to benchmark the Maps audit.
type Competitor struct {
	Reviews int     `db:"reviews"`
	Rating  float64 `db:"rating"`
}

// auditMap: Google Maps profile audit benchmarked against the top competitors of the same search.
func auditMap(l AuditLead, comp []Competitor) *Result {
	var ch checks
	rating, rev := l.Rating, l.Reviews
	avgRev, avgRat := 0, 0.0
	if len(comp) > 0 {
		sr, sa := 0, 0.0
		for _, c := range comp {
			sr += c.Reviews
			sa += c.Rating
		}
		avgRev = int(math.Round(float64(sr) / float64(len(comp))))
		avgRat = rnd(sa/float64(len(comp)), 1)
	}
	rd := "No rating yet."
	if rating > 0 {
		rd = fmtF(rating, 1) + " ★ average rating"
		if avgRat > 0 {
			rd += " (top competitors: " + fmtF(avgRat, 1) + " ★)"
		}
		rd += ". " + pick(rating < 4.5, "Customers filter by rating – 4.5★+ wins most clicks.", "Excellent.")
	}
	ch.add("Google rating", tri(rating >= 4.5, rating >= 4), rd, 3, "Reputation")
	vd := strconv.Itoa(rev) + " reviews"
	if avgRev > 0 {
		vd += " vs. top competitors' average of " + strconv.Itoa(avgRev)
		if avgRev > rev {
			vd += " – a gap of " + strconv.Itoa(avgRev-rev) + " reviews"
		}
	}
	ch.add("Number of reviews", tri(rev >= max(100, avgRev), rev >= 30), vd+". Review count is one of the strongest Google Maps ranking factors.", 3, "Reputation")
	if pos := l.Position; pos > 0 {
		ch.add("Google Maps ranking position", tri(pos <= 3, pos <= 10), fmt.Sprintf(`Ranked #%d for "%s". `, pos, l.Keyword)+pick(pos > 3, "The top 3 (local pack) receive most calls and direction requests.", "In the top 3 – great."), 3, "Visibility")
	}
	web := l.Website != ""
	ch.add("Website linked on profile", pick(web, "pass", "fail"), pick(web, "Website is linked.", "No website on the Google profile – customers can't learn more and Google trusts the listing less."), 3, "Profile")
	pm := l.PhoneSource == "maps"
	ch.add("Phone number on profile", pick(pm, "pass", "fail"), pick(pm, "Phone number listed.", "No phone number on the Google listing – direct calls are lost."), 3, "Profile")
	ph := l.Raw.Photos
	phs := strconv.Itoa(ph)
	if ph >= 10 {
		phs = "10+"
	}
	ch.add("Photos", tri(ph >= 10, ph >= 4), phs+" photos found. Listings with many fresh photos get far more direction requests and clicks.", 2, "Profile")
	hrs := len(l.Raw.Hours) > 0
	ch.add("Business hours", pick(hrs, "pass", "fail"), pick(hrs, "Opening hours listed.", `No opening hours – Google may hide the listing for "open now" searches.`), 2, "Profile")
	desc := l.Raw.Description != ""
	ch.add("Business description", pick(desc, "pass", "warn"), pick(desc, "Description present.", "No visible business description / editorial summary."), 1, "Profile")
	cat := l.Category != ""
	ch.add("Primary category", pick(cat, "pass", "warn"), pick(cat, "Category: "+l.Category+".", "No clear primary category."), 1, "Profile")
	if n := len(l.Raw.Types); n > 0 {
		ch.add("Secondary categories", pick(n >= 3, "pass", "warn"), fmt.Sprintf("%d business categories set. Adding all relevant secondary categories helps appear in more searches.", n), 1, "Visibility")
	}
	if st := l.Raw.Status; st != "" && st != "OPERATIONAL" {
		ch.add("Business status", "fail", "Listing status is "+st+".", 3, "Profile")
	}
	return &Result{Score: score(ch), Checks: ch, Cats: catScores(ch, mapCats), Meta: map[string]any{"rating": rating, "reviews": rev, "photos": ph, "position": l.Position, "competitor_avg_reviews": avgRev, "competitor_avg_rating": avgRat}}
}

