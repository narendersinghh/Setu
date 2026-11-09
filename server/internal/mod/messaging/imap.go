package messaging

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset" // non-UTF-8 emails (ISO-8859-x, Windows-125x…)
	gomail "github.com/emersion/go-message/mail"

	"github.com/narendersinghh/setu/server/internal/settings"
)

// imapOn: replies are read from the inbox (setting imap_on=1 and a server).
func imapOn(s *settings.Store) bool { return s.Get("imap_on", "0") == "1" && s.Get("imap_host", "") != "" }

func imapLogin(s *settings.Store) (string, string) {
	return s.Get("imap_user", s.Get("smtp_user", s.Get("from_email", ""))), s.Get("imap_pass", s.Get("smtp_pass", ""))
}

// imapConnect logs in (SSL on 993, otherwise STARTTLS when the server offers it).
func imapConnect(ctx context.Context, s *settings.Store) (*client.Client, error) {
	host, port := s.Get("imap_host", ""), s.Get("imap_port", "993")
	if host == "" {
		return nil, errors.New("Enter the IMAP server in Settings → Messaging → Email.")
	}
	conn, err := dialPublic(ctx, host, port)
	if err != nil {
		return nil, fmt.Errorf("Could not connect to the inbox (%s:%s): %v", host, port, err)
	}
	conn.SetDeadline(time.Now().Add(40 * time.Second))
	tc := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if port == "993" {
		t := tls.Client(conn, tc)
		if err := t.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("Could not connect to the inbox (%s:%s): %v", host, port, err)
		}
		conn = t
	}
	c, err := client.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("Could not connect to the inbox (%s:%s): %v", host, port, err)
	}
	c.Timeout = 40 * time.Second
	if port != "993" {
		if ok, _ := c.SupportStartTLS(); ok {
			if err := c.StartTLS(tc); err != nil {
				c.Terminate()
				return nil, fmt.Errorf("STARTTLS to the inbox failed: %v", err)
			}
		}
	}
	u, p := imapLogin(s)
	if err := c.Login(u, p); err != nil {
		c.Terminate()
		return nil, errors.New("Inbox login failed – check the IMAP login and password.")
	}
	return c, nil
}

// testInbox returns the number of messages in INBOX.
func testInbox(ctx context.Context, s *settings.Store) (int, error) {
	c, err := imapConnect(ctx, s)
	if err != nil {
		return 0, err
	}
	defer c.Logout()
	mb, err := c.Select("INBOX", true)
	if err != nil {
		return 0, err
	}
	return int(mb.Messages), nil
}

// inMail is one parsed received email.
type inMail struct {
	UID        uint32
	From       string
	FromName   string
	To         string
	Subject    string
	Date       time.Time
	MsgID      string
	InReplyTo  string
	References string
	Auto       bool // auto-reply / bulk
	Text       string
}

// fetchNew returns new messages since the last check (remembered as imap_uid / imap_uidv).
// The caller saves the returned cursor.
func fetchNew(ctx context.Context, s *settings.Store, max int) ([]inMail, uint32, string, error) {
	c, err := imapConnect(ctx, s)
	if err != nil {
		return nil, 0, "", err
	}
	defer c.Logout()
	mb, err := c.Select("INBOX", true)
	if err != nil {
		return nil, 0, "", err
	}
	uv := strconv.FormatUint(uint64(mb.UidValidity), 10)
	last := uint32(s.Int("imap_uid", 0))
	if s.Get("imap_uidv", "") != uv {
		last = 0
	}
	cr := imap.NewSearchCriteria()
	if last > 0 {
		set := new(imap.SeqSet)
		set.AddRange(last+1, 0)
		cr.Uid = set
	} else {
		cr.Since = time.Now().AddDate(0, 0, -14)
	}
	uids, err := c.UidSearch(cr)
	if err != nil {
		return nil, 0, "", err
	}
	var todo []uint32
	for _, u := range uids {
		if u > last {
			todo = append(todo, u)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i] < todo[j] })
	if len(todo) > max {
		todo = todo[:max]
	}
	if len(todo) == 0 {
		return nil, last, uv, nil
	}
	set := new(imap.SeqSet)
	set.AddNum(todo...)
	sec := &imap.BodySectionName{Peek: true, Partial: []int{0, 400000}}
	ch := make(chan *imap.Message, len(todo))
	errc := make(chan error, 1)
	go func() { errc <- c.UidFetch(set, []imap.FetchItem{imap.FetchUid, sec.FetchItem()}, ch) }()
	var out []inMail
	for m := range ch {
		var lit imap.Literal
		for _, l := range m.Body { // the server answers BODY[]<0>, which does not equal the requested section
			if l != nil {
				lit = l
				break
			}
		}
		if lit == nil {
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(lit, 500000))
		p := parseMail(raw)
		p.UID = m.Uid
		out = append(out, p)
		if m.Uid > last {
			last = m.Uid
		}
	}
	if err := <-errc; err != nil && len(out) == 0 {
		return nil, 0, "", err
	}
	for _, u := range todo {
		if u > last {
			last = u
		}
	}
	return out, last, uv, nil
}

// parseMail reads headers and the best text body (plain, else HTML → text) without the quoted
// earlier conversation (v1 Mailer::parse).
func parseMail(raw []byte) inMail {
	var m inMail
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if r == nil {
		m.Subject = "(no subject)"
		m.Date = time.Now()
		_ = err
		return m
	}
	h := r.Header
	if l, err := h.AddressList("From"); err == nil && len(l) > 0 {
		m.From, m.FromName = strings.ToLower(l[0].Address), l[0].Name
	} else {
		f := h.Get("From")
		if i, j := strings.IndexByte(f, '<'), strings.IndexByte(f, '>'); i >= 0 && j > i {
			m.From = strings.ToLower(strings.TrimSpace(f[i+1 : j]))
		} else {
			m.From = strings.ToLower(strings.Trim(f, ` "<>`))
		}
	}
	if to, err := h.Text("To"); err == nil {
		m.To = to
	}
	m.Subject, _ = h.Subject()
	if m.Subject == "" {
		m.Subject = "(no subject)"
	}
	if d, err := h.Date(); err == nil && !d.IsZero() && d.Before(time.Now().Add(time.Hour)) {
		m.Date = d
	} else {
		m.Date = time.Now()
	}
	m.MsgID = strings.TrimSpace(h.Get("Message-Id"))
	m.InReplyTo = strings.TrimSpace(h.Get("In-Reply-To"))
	m.References = strings.TrimSpace(h.Get("References"))
	as := h.Get("Auto-Submitted")
	m.Auto = (as != "" && !strings.HasPrefix(strings.ToLower(as), "no")) || strings.Contains(strings.ToLower(h.Get("Precedence")), "bulk")
	var plain, htm string
	for {
		p, err := r.NextPart()
		if err == io.EOF || (err != nil && p == nil) {
			break
		}
		ih, ok := p.Header.(*gomail.InlineHeader)
		if !ok {
			continue
		}
		t, _, _ := ih.ContentType()
		b, _ := io.ReadAll(io.LimitReader(p.Body, 400000))
		switch {
		case (t == "text/plain" || t == "") && plain == "":
			plain = string(b)
		case t == "text/html" && htm == "":
			htm = string(b)
		}
	}
	body := plain
	if strings.TrimSpace(body) == "" {
		body = html2text(htm)
	}
	m.Text = stripQuoted(body)
	return m
}

var (
	reScript   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reHead     = regexp.MustCompile(`(?is)<head[^>]*>.*?</head>`)
	reQuote    = regexp.MustCompile(`(?is)<blockquote.*$`)
	reGmail    = regexp.MustCompile(`(?is)<div class="?gmail_quote.*$`)
	reBreaks   = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/tr|/h\d)[^>]*>`)
	reTags     = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank    = regexp.MustCompile(`\n{3,}`)
	reHSpace   = regexp.MustCompile(`[ \t]+\n`)
	quotedCuts = []*regexp.Regexp{
		regexp.MustCompile(`(?m)^On .{4,200}wrote:\s*$`),
		regexp.MustCompile(`(?mi)^-{2,}\s*Original Message\s*-{2,}`),
		regexp.MustCompile(`(?m)^_{10,}\s*$`),
		regexp.MustCompile(`(?m)^From: .+\nSent: `),
		regexp.MustCompile(`(?m)^\s*>`),
	}
)

func html2text(h string) string {
	for _, re := range []*regexp.Regexp{reScript, reStyle, reHead, reQuote, reGmail} {
		h = re.ReplaceAllString(h, "")
	}
	h = reBreaks.ReplaceAllString(h, "\n")
	h = html.UnescapeString(reTags.ReplaceAllString(h, ""))
	return strings.TrimSpace(reHSpace.ReplaceAllString(h, "\n"))
}

// stripQuoted cuts the quoted earlier conversation ("On … wrote:", "> …", "-----Original Message-----").
func stripQuoted(t string) string {
	t = strings.ReplaceAll(t, "\r\n", "\n")
	for _, re := range quotedCuts {
		if loc := re.FindStringIndex(t); loc != nil && loc[0] > 0 {
			t = t[:loc[0]]
		}
	}
	return strings.TrimSpace(reBlank.ReplaceAllString(t, "\n\n"))
}
