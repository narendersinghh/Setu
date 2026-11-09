// Package netx: outbound HTTP. Public() blocks private/loopback/link-local/metadata addresses at
// dial time on every hop (SSRF guard) – use it for any URL that comes from users or third parties.
package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

var ErrBlocked = errors.New("blocked: not a public web address")

// IsPublic reports a globally routable unicast address.
func IsPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var blocked = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32", "fc00::/7"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func guard(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !IsPublic(ip) {
		return ErrBlocked
	}
	return nil
}

func newClient(public bool, timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if public {
		d.Control = guard
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         d.DialContext,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if public {
		tr.Proxy = nil // a proxy would hide the real destination from the guard
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
			return ErrBlocked
		}
		return nil
	}}
}

var (
	apiClient    = newClient(false, 0)
	publicClient = newClient(true, 0)
)

type Req struct {
	Method  string
	URL     string
	Headers map[string]string
	JSON    any       // encoded as the body
	Body    io.Reader // raw body
	Timeout time.Duration
	Max     int64 // max body bytes (0 = 20 MB)
	Public  bool  // third-party URL: SSRF guard on
}

type Resp struct {
	Code     int
	Body     []byte
	Header   http.Header
	URL      string // final URL after redirects
	Duration time.Duration
	TTFB     time.Duration
}

func (r *Resp) OK() bool { return r.Code >= 200 && r.Code < 300 }

// JSON decodes the body.
func (r *Resp) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// Do performs the request.
func Do(ctx context.Context, q Req) (*Resp, error) {
	if q.Method == "" {
		q.Method = http.MethodGet
		if q.JSON != nil || q.Body != nil {
			q.Method = http.MethodPost
		}
	}
	if q.Timeout == 0 {
		q.Timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, q.Timeout)
	defer cancel()
	body := q.Body
	if q.JSON != nil {
		b, err := json.Marshal(q.JSON)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, q.Method, q.URL, body)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(req.URL.Scheme, "http") {
		return nil, ErrBlocked
	}
	req.Header.Set("User-Agent", UA)
	if q.JSON != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range q.Headers {
		req.Header.Set(k, v)
	}
	c := apiClient
	if q.Public {
		c = publicClient
	}
	t0 := time.Now()
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	ttfb := time.Since(t0)
	mx := q.Max
	if mx <= 0 {
		mx = 20 << 20
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, mx))
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	return &Resp{Code: res.StatusCode, Body: b, Header: res.Header, URL: res.Request.URL.String(), Duration: time.Since(t0), TTFB: ttfb}, nil
}

// GetJSON is a shortcut for API calls.
func GetJSON(ctx context.Context, url string, headers map[string]string, out any) (*Resp, error) {
	r, err := Do(ctx, Req{URL: url, Headers: headers})
	if err != nil {
		return nil, err
	}
	if err := r.JSON(out); err != nil {
		return r, fmt.Errorf("HTTP %d: %w", r.Code, err)
	}
	return r, nil
}
