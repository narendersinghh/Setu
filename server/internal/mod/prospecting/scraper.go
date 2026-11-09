package prospecting

import (
	"context"
	"html"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/netx"
)

// Outbound HTTP. Website fetches always use the SSRF guard (Public); tests may replace these.
var (
	webDo = func(ctx context.Context, r netx.Req) (*netx.Resp, error) { r.Public = true; return netx.Do(ctx, r) }
	apiDo = func(ctx context.Context, r netx.Req) (*netx.Resp, error) { return netx.Do(ctx, r) }
)

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

var schemeRe = regexp.MustCompile(`(?i)^https?://`)

// normURL adds http:// to a bare domain.
func normURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if !schemeRe.MatchString(u) {
		u = "http://" + u
	}
	return u
}

// fetchSite opens a website (max 3 MB, 25 s).
func fetchSite(ctx context.Context, u string) (*netx.Resp, error) {
	return webDo(ctx, netx.Req{URL: normURL(u), Timeout: secs(25), Max: 3_000_000})
}

// Contacts found on a website.
type Contacts struct {
	Phones   []string          `json:"phones"`
	Emails   []string          `json:"emails"`
	Socials  map[string]string `json:"socials"`
	WhatsApp string            `json:"whatsapp"`
}

var contactLinkRe = regexp.MustCompile(`(?i)href=["']([^"']*(contact|reach|about)[^"']*)["']`)

// scanContacts finds phones, emails and social links on a website (home + contact page).
func scanContacts(ctx context.Context, site string) Contacts {
	res := Contacts{Phones: []string{}, Emails: []string{}, Socials: map[string]string{}}
	r, err := fetchSite(ctx, site)
	if err != nil || r.Code < 200 || r.Code >= 400 || len(r.Body) == 0 {
		return res
	}
	pages := []string{string(r.Body)}
	if m := contactLinkRe.FindStringSubmatch(string(r.Body)); m != nil {
		link := absURL(m[1], r.URL)
		if link != "" && hostOf(link) == hostOf(r.URL) {
			if c, err := fetchSite(ctx, link); err == nil && c.Code == 200 {
				pages = append(pages, string(c.Body))
			}
		}
	}
	for _, h := range pages {
		extract(h, &res)
	}
	res.Phones = uniq(res.Phones)
	res.Emails = uniq(res.Emails)
	return res
}

var (
	telRe      = regexp.MustCompile(`(?i)href=["']tel:([^"']+)["']`)
	waRe       = regexp.MustCompile(`(?i)(?:wa\.me/|api\.whatsapp\.com/send\?phone=|whatsapp://send\?phone=)\+?(\d{8,15})`)
	phoneRe    = regexp.MustCompile(`(\+?\d{1,3}[\s.-]?)?(\(?\d{2,5}\)?[\s.-]?)\d{3,5}[\s.-]?\d{3,5}`)
	dateLikeRe = regexp.MustCompile(`^(19|20)\d{6}$`)
	mailtoRe   = regexp.MustCompile(`(?i)mailto:([^"'?\s>]+)`)
	emailRe    = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,10}`)
	badEmailRe = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|svg|css|js)$|example\.|sentry|wixpress|domain\.com|yourdomain|email\.com|@2x|godaddy`)
	shareRe    = regexp.MustCompile(`(?i)sharer|share\?|intent/`)
	socialRes  = []struct {
		k  string
		re *regexp.Regexp
	}{
		{"facebook", regexp.MustCompile(`(?i)https?://(?:www\.)?facebook\.com/[^"'\s<>]+`)},
		{"instagram", regexp.MustCompile(`(?i)https?://(?:www\.)?instagram\.com/[^"'\s<>]+`)},
		{"linkedin", regexp.MustCompile(`(?i)https?://(?:www\.)?linkedin\.com/[^"'\s<>]+`)},
		{"youtube", regexp.MustCompile(`(?i)https?://(?:www\.)?youtube\.com/[^"'\s<>]+`)},
		{"twitter", regexp.MustCompile(`(?i)https?://(?:www\.)?(?:twitter|x)\.com/[^"'\s<>]+`)},
	}
	scriptRe   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	noscriptRe = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`)
	tagRe      = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe    = regexp.MustCompile(`\s+`)
	nonDigit   = regexp.MustCompile(`\D`)
)

// pageText: visible text of an HTML page (scripts, styles and tags removed, entities decoded).
func pageText(h string, noscript bool) string {
	h = scriptRe.ReplaceAllString(h, " ")
	h = styleRe.ReplaceAllString(h, " ")
	if noscript {
		h = noscriptRe.ReplaceAllString(h, " ")
	}
	return html.UnescapeString(tagRe.ReplaceAllString(h, ""))
}

func digits(s string) string { return nonDigit.ReplaceAllString(s, "") }

func isDigitOrSlash(b byte) bool { return b == '/' || b >= '0' && b <= '9' }

func extract(h string, res *Contacts) {
	for _, m := range telRe.FindAllStringSubmatch(h, -1) {
		p, err := url.QueryUnescape(m[1])
		if err != nil {
			p = m[1]
		}
		if len(digits(p)) >= 8 {
			res.Phones = append(res.Phones, strings.TrimSpace(p))
		}
	}
	if m := waRe.FindStringSubmatch(h); m != nil {
		res.WhatsApp = m[1]
		res.Phones = append(res.Phones, "+"+m[1])
	}
	text := pageText(h, false)
	if len(res.Phones) == 0 {
		for _, ix := range phoneRe.FindAllStringIndex(text, -1) {
			// emulate (?<![\d/]) … (?![\d/])
			if ix[0] > 0 && isDigitOrSlash(text[ix[0]-1]) || ix[1] < len(text) && isDigitOrSlash(text[ix[1]]) {
				continue
			}
			p := text[ix[0]:ix[1]]
			d := digits(p)
			if len(d) >= 10 && len(d) <= 13 && !dateLikeRe.MatchString(d) {
				res.Phones = append(res.Phones, strings.TrimSpace(p))
			}
		}
	}
	if len(res.Phones) > 5 {
		res.Phones = res.Phones[:5]
	}
	for _, m := range mailtoRe.FindAllStringSubmatch(h, -1) {
		x, err := url.QueryUnescape(m[1])
		if err != nil {
			x = m[1]
		}
		res.Emails = append(res.Emails, strings.ToLower(x))
	}
	for _, x := range emailRe.FindAllString(text+" "+h, -1) {
		res.Emails = append(res.Emails, strings.ToLower(x))
	}
	var ok []string
	for _, x := range uniq(res.Emails) {
		if validEmail(x) && !badEmailRe.MatchString(x) {
			ok = append(ok, x)
		}
	}
	res.Emails = ok
	if res.Emails == nil {
		res.Emails = []string{}
	}
	for _, s := range socialRes {
		if res.Socials[s.k] != "" {
			continue
		}
		if m := s.re.FindString(h); m != "" && !shareRe.MatchString(m) {
			res.Socials[s.k] = m
		}
	}
}

var simpleEmail = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$`)

func validEmail(e string) bool {
	if !simpleEmail.MatchString(e) {
		return false
	}
	_, err := mail.ParseAddress(e)
	return err == nil
}

func uniq(a []string) []string {
	seen := make(map[string]bool, len(a))
	out := make([]string, 0, len(a))
	for _, x := range a {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return p.Hostname()
}

var skipHref = regexp.MustCompile(`(?i)^(mailto|tel|javascript|#)`)

// absURL resolves a link against the page address (same rules as v1 Scraper::abs).
func absURL(href, base string) string {
	href = strings.TrimSpace(href)
	if href == "" || skipHref.MatchString(href) {
		return ""
	}
	if schemeRe.MatchString(href) {
		return href
	}
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		return b.Scheme + ":" + href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return b.ResolveReference(ref).String()
}
