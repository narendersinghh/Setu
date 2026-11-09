package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/narendersinghh/setu/server/internal/netx"
)

// ---------------------------------------------------------------- HTTP clients

// Streaming needs the response body as it arrives, so this package keeps its own clients (netx.Do reads
// the whole body). The guarded client blocks private / loopback / metadata addresses at dial time for
// addresses typed by users (custom endpoint, Claude gateway).
func guardDial(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !netx.IsPublic(ip) {
		return netx.ErrBlocked
	}
	return nil
}

func newClient(guard bool) *http.Client {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: d.DialContext, MaxIdleConns: 200, MaxIdleConnsPerHost: 50,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true, ResponseHeaderTimeout: 0}
	if guard {
		d.Control = guardDial
		tr.Proxy = nil
	}
	return &http.Client{Transport: tr, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
			return netx.ErrBlocked
		}
		return nil
	}}
}

var (
	apiClient     = newClient(false)
	guardedClient = newClient(true)
)

// allowPrivate switches the SSRF guard off (tests only: httptest servers listen on 127.0.0.1).
var allowPrivate = false

func clientFor(c conn) *http.Client {
	if c.Guard && !allowPrivate {
		return guardedClient
	}
	return apiClient
}

// ---------------------------------------------------------------- errors

// apiError is a failed AI request. Code 0 = no answer (network error / timeout).
type apiError struct {
	Code       int
	Msg        string
	RetryAfter time.Duration
	Retryable  bool
}

func (e *apiError) Error() string { return e.Msg }

var reBusy = regexp.MustCompile(`(?i)overload|rate|timeout|busy|capacity`)

// retryable: rate limit, overload, server error or no answer.
func retryable(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	if ae.Retryable {
		return true
	}
	switch ae.Code {
	case 0, 408, 429, 500, 502, 503, 504, 529:
		return true
	}
	return false
}

// errMsg finds the error message in a provider error body (v1 Parallel::msg).
func errMsg(raw []byte) string {
	var d any
	if json.Unmarshal(raw, &d) == nil {
		if a, ok := d.([]any); ok && len(a) > 0 {
			d = a[0]
		}
		if m, ok := d.(map[string]any); ok {
			if e, ok := m["error"].(map[string]any); ok {
				if s, ok := e["message"].(string); ok && s != "" {
					return s
				}
			}
			if s, ok := m["error"].(string); ok && s != "" {
				return s
			}
			if s, ok := m["message"].(string); ok && s != "" {
				return s
			}
		}
	}
	s := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(string(raw), "")
	return cut(strings.TrimSpace(s), 200)
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// ---------------------------------------------------------------- one call

// callOpts is one AI request (provider independent).
type callOpts struct {
	System      string
	Messages    []Msg
	Max         int
	Temperature float64
	JSON        bool
	WebSearch   bool
	Searches    int
	NoRetry     bool                // the bulk engine retries itself (frees the slot meanwhile)
	OnDelta     func(text string)   // streamed text
	OnNote      func(detail string) // e.g. "provider busy – retrying"
}

// result of a call; tokens are counted even when the call failed half way.
type result struct {
	Text        string
	In, Out     int
	WebSearches int
	Turns       int
	Stop        string
	Sources     []Source
}

// Wait before retrying a busy provider: Retry-After, else retryBase × attempt, kept within retryMin–15s
// (tests lower both).
var (
	retryMin  = 3 * time.Second
	retryBase = 6 * time.Second
)

// call sends one request with streaming, retrying a busy provider while there is time left.
func call(ctx context.Context, c conn, o callOpts) (*result, error) {
	if o.Max <= 0 {
		o.Max = 4096
	}
	if o.JSON {
		o.System = strings.TrimSpace(o.System + "\n\nReply with one valid JSON object only – no text before or after it.")
	}
	total := &result{}
	for attempt := 0; ; attempt++ {
		r, err := callOnce(ctx, c, o)
		total.In += r.In
		total.Out += r.Out
		total.WebSearches += r.WebSearches
		total.Turns += max(1, r.Turns)
		if err == nil {
			total.Text, total.Stop, total.Sources = r.Text, r.Stop, r.Sources
			return total, nil
		}
		if o.NoRetry || attempt >= 2 || !retryable(err) || ctx.Err() != nil {
			return total, err
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < 90*time.Second {
			return total, err
		}
		var ae *apiError
		errors.As(err, &ae)
		wait := ae.RetryAfter
		if wait == 0 {
			wait = retryBase * time.Duration(attempt+1)
		}
		wait = min(15*time.Second, max(retryMin, wait))
		if o.OnNote != nil {
			code := "no answer"
			if ae.Code > 0 {
				code = "HTTP " + strconv.Itoa(ae.Code)
			}
			o.OnNote("The AI provider is busy (" + code + ") – retrying")
		}
		select {
		case <-ctx.Done():
			return total, err
		case <-time.After(wait):
		}
	}
}

func callOnce(ctx context.Context, c conn, o callOpts) (*result, error) {
	switch c.Provider {
	case "anthropic":
		return anthropicCall(ctx, c, o)
	case "gemini":
		return geminiCall(ctx, c, o)
	default:
		return openAICall(ctx, c, o)
	}
}

// post sends a JSON request and returns the open response (the caller closes the body). Non-2xx answers
// are turned into apiError.
func post(ctx context.Context, c conn, url string, headers map[string]string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "Setu/2")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := clientFor(c).Do(req)
	if err != nil {
		return nil, &apiError{Code: 0, Msg: netMsg(ctx, err)}
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		res.Body.Close()
		ae := &apiError{Code: res.StatusCode, Msg: errMsg(raw)}
		if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s > 0 {
			ae.RetryAfter = time.Duration(s) * time.Second
		}
		return nil, ae
	}
	return res, nil
}

func netMsg(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, netx.ErrBlocked) {
		return "the address is not a public web address"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return err.Error()
}

// sse reads Server-Sent Events and calls fn with each JSON data payload. A body that is plain JSON
// (endpoints that ignore "stream") is passed to fn whole.
func sse(res *http.Response, fn func(data []byte) error) error {
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); strings.Contains(ct, "json") && !strings.Contains(ct, "event-stream") {
		b, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
		if err != nil {
			return err
		}
		return fn(bytes.TrimSpace(b))
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 32<<20)
	var data []byte
	flush := func() error {
		d := bytes.TrimSpace(data)
		data = data[:0]
		if len(d) == 0 || string(d) == "[DONE]" {
			return nil
		}
		return fn(d)
	}
	for sc.Scan() {
		ln := sc.Bytes()
		if len(ln) == 0 {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if bytes.HasPrefix(ln, []byte("data:")) {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimPrefix(bytes.TrimPrefix(ln, []byte("data:")), []byte(" "))...)
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return sc.Err()
}

// streamErr turns a broken stream into an apiError.
func streamErr(ctx context.Context, err error) error {
	var ae *apiError
	if errors.As(err, &ae) {
		return err
	}
	return &apiError{Code: 0, Msg: netMsg(ctx, err)}
}

// ---------------------------------------------------------------- Anthropic Messages API

func anthropicHeaders(c conn) map[string]string {
	return map[string]string{"x-api-key": c.Key, "anthropic-version": anthropicVersion}
}

// anthropicBody builds the Messages API request (without stream / messages).
func anthropicBody(model, system string, max int, temp float64) map[string]any {
	b := map[string]any{"model": model, "max_tokens": max}
	if system != "" {
		b["system"] = system
	}
	if temp > 0 {
		b["temperature"] = temp
	}
	if reEffort.MatchString(model) {
		b["output_config"] = map[string]any{"effort": "medium"}
	}
	return b
}

func anthropicMsgs(ms []Msg) []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		out = append(out, map[string]any{"role": role, "content": m.Content})
	}
	return out
}

func anthropicCall(ctx context.Context, c conn, o callOpts) (*result, error) {
	body := anthropicBody(c.Model, o.System, o.Max, o.Temperature)
	if o.WebSearch {
		body["tools"] = []any{webSearchTool(c.Model, o.Searches)}
	}
	fb := reFallback.MatchString(c.Model)
	msgs := anthropicMsgs(o.Messages)
	r := &result{}
	var text strings.Builder
	for turn := 0; turn < 4; turn++ {
		if turn > 0 {
			if dl, ok := ctx.Deadline(); ok && time.Until(dl) < 45*time.Second {
				break
			}
		}
		req := map[string]any{"messages": msgs, "stream": true}
		for k, v := range body {
			req[k] = v
		}
		h := anthropicHeaders(c)
		if fb {
			req["fallbacks"] = "default"
			h["anthropic-beta"] = "server-side-fallback-2026-07-01"
		}
		res, err := post(ctx, c, c.Base+"/v1/messages", h, req)
		var ae *apiError
		if fb && errors.As(err, &ae) && ae.Code == 400 {
			// Server-side fallback not available for this account / model: plain request
			fb = false
			delete(req, "fallbacks")
			res, err = post(ctx, c, c.Base+"/v1/messages", anthropicHeaders(c), req)
		}
		if _, ok := body["tools"]; ok && errors.As(err, &ae) && ae.Code == 400 {
			delete(body, "tools")
			delete(req, "tools")
			if o.OnNote != nil {
				o.OnNote("Web search is not available for this model – continuing without it")
			}
			res, err = post(ctx, c, c.Base+"/v1/messages", anthropicHeaders(c), req)
		}
		r.Turns = turn + 1
		if err != nil {
			return r, claudeErr(err)
		}
		st := &anthropicStream{onDelta: o.OnDelta}
		err = sse(res, st.event)
		r.In += st.in
		r.Out += st.out
		r.WebSearches += st.searches
		if err == nil && st.err != nil {
			err = st.err
		}
		if err != nil {
			return r, claudeErr(streamErr(ctx, err))
		}
		t, src := anthropicText(st.blocks)
		text.WriteString(t)
		r.Sources = mergeSources(r.Sources, src)
		r.Stop = st.stop
		if st.stop == "refusal" {
			cat := st.refusal
			if cat == "" {
				cat = "policy"
			}
			return r, &apiError{Code: 200, Msg: "Claude declined this request (" + cat + "). Try again or edit the prompt."}
		}
		if st.stop != "pause_turn" {
			break
		}
		// Long web research: continue the paused turn
		msgs = append(msgs, map[string]any{"role": "assistant", "content": st.blocks})
	}
	r.Text = text.String()
	if strings.TrimSpace(r.Text) == "" {
		return r, &apiError{Code: 200, Msg: "Claude returned an empty response."}
	}
	return r, nil
}

func claudeErr(err error) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	out := *ae
	if ae.Code == 0 {
		out.Msg = "Claude did not answer in time (" + ae.Msg + "). Try again, or choose a faster model (e.g. claude-sonnet-5-5) in AI Connection."
	} else if !strings.HasPrefix(ae.Msg, "Claude") {
		out.Msg = "Claude API error " + strconv.Itoa(ae.Code) + ": " + ae.Msg
	}
	return &out
}

// anthropicStream rebuilds the content blocks of a streamed message.
type anthropicStream struct {
	onDelta  func(string)
	blocks   []map[string]any
	partial  map[int]*strings.Builder
	in, out  int
	searches int
	stop     string
	refusal  string
	err      error
}

func (s *anthropicStream) block(i int) map[string]any {
	for len(s.blocks) <= i {
		s.blocks = append(s.blocks, map[string]any{})
	}
	return s.blocks[i]
}

type aUsage struct {
	In            int `json:"input_tokens"`
	CacheCreate   int `json:"cache_creation_input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	Out           int `json:"output_tokens"`
	ServerToolUse struct {
		Searches int `json:"web_search_requests"`
	} `json:"server_tool_use"`
}

func (u aUsage) input() int { return u.In + u.CacheCreate + u.CacheRead }

func (s *anthropicStream) event(data []byte) error {
	var e struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message *struct {
			Usage aUsage `json:"usage"`
		} `json:"message"`
		ContentBlock map[string]any `json:"content_block"`
		Delta        struct {
			Type        string         `json:"type"`
			Text        string         `json:"text"`
			PartialJSON string         `json:"partial_json"`
			Citation    map[string]any `json:"citation"`
			Thinking    string         `json:"thinking"`
			Signature   string         `json:"signature"`
			StopReason  string         `json:"stop_reason"`
			StopDetails *struct {
				Category string `json:"category"`
			} `json:"stop_details"`
		} `json:"delta"`
		Usage *aUsage `json:"usage"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		// non-streamed answer (endpoint ignored "stream")
		Content     []map[string]any `json:"content"`
		StopReason  string           `json:"stop_reason"`
		StopDetails *struct {
			Category string `json:"category"`
		} `json:"stop_details"`
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return nil // keep-alive or unknown line
	}
	switch e.Type {
	case "message_start":
		if e.Message != nil {
			s.in = e.Message.Usage.input()
			s.out = e.Message.Usage.Out
		}
	case "content_block_start":
		b := s.block(e.Index)
		for k, v := range e.ContentBlock {
			b[k] = v
		}
		if t, _ := b["text"].(string); t != "" && s.onDelta != nil {
			s.onDelta(t)
		}
	case "content_block_delta":
		b := s.block(e.Index)
		switch e.Delta.Type {
		case "text_delta":
			t, _ := b["text"].(string)
			b["text"] = t + e.Delta.Text
			if s.onDelta != nil && e.Delta.Text != "" {
				s.onDelta(e.Delta.Text)
			}
		case "input_json_delta":
			if s.partial == nil {
				s.partial = map[int]*strings.Builder{}
			}
			if s.partial[e.Index] == nil {
				s.partial[e.Index] = &strings.Builder{}
			}
			s.partial[e.Index].WriteString(e.Delta.PartialJSON)
		case "citations_delta":
			cs, _ := b["citations"].([]any)
			b["citations"] = append(cs, e.Delta.Citation)
		case "thinking_delta":
			t, _ := b["thinking"].(string)
			b["thinking"] = t + e.Delta.Thinking
		case "signature_delta":
			b["signature"] = e.Delta.Signature
		}
	case "content_block_stop":
		if p := s.partial[e.Index]; p != nil {
			var in any = map[string]any{}
			if p.Len() > 0 {
				json.Unmarshal([]byte(p.String()), &in)
			}
			s.block(e.Index)["input"] = in
			delete(s.partial, e.Index)
		}
	case "message_delta":
		if e.Delta.StopReason != "" {
			s.stop = e.Delta.StopReason
		}
		if e.Delta.StopDetails != nil {
			s.refusal = e.Delta.StopDetails.Category
		}
		if e.Usage != nil {
			if e.Usage.Out > 0 {
				s.out = e.Usage.Out
			}
			if in := e.Usage.input(); in > s.in {
				s.in = in
			}
			s.searches = max(s.searches, e.Usage.ServerToolUse.Searches)
		}
	case "error":
		if e.Error != nil {
			s.err = &apiError{Code: 0, Msg: e.Error.Message, Retryable: reBusy.MatchString(e.Error.Type + " " + e.Error.Message)}
			if s.err.(*apiError).Msg == "" {
				s.err.(*apiError).Msg = e.Error.Type
			}
		}
	case "message":
		s.blocks = e.Content
		s.stop = e.StopReason
		if e.StopDetails != nil {
			s.refusal = e.StopDetails.Category
		}
		if e.Usage != nil {
			s.in, s.out, s.searches = e.Usage.input(), e.Usage.Out, e.Usage.ServerToolUse.Searches
		}
		if s.onDelta != nil {
			t, _ := anthropicText(s.blocks)
			s.onDelta(t)
		}
	}
	return nil
}

// anthropicText joins the text blocks and collects the web sources (cited first, then every result).
func anthropicText(blocks []map[string]any) (string, []Source) {
	var b strings.Builder
	var cited, found []Source
	for _, bl := range blocks {
		switch bl["type"] {
		case "text":
			t, _ := bl["text"].(string)
			b.WriteString(t)
			cs, _ := bl["citations"].([]any)
			for _, c := range cs {
				if m, ok := c.(map[string]any); ok {
					u, _ := m["url"].(string)
					ti, _ := m["title"].(string)
					if u != "" {
						cited = append(cited, Source{Title: ti, URL: u})
					}
				}
			}
		case "web_search_tool_result":
			rs, _ := bl["content"].([]any)
			for _, x := range rs {
				if m, ok := x.(map[string]any); ok && m["type"] == "web_search_result" {
					u, _ := m["url"].(string)
					ti, _ := m["title"].(string)
					if u != "" {
						found = append(found, Source{Title: ti, URL: u})
					}
				}
			}
		}
	}
	return b.String(), mergeSources(cited, found)
}

func mergeSources(a, b []Source) []Source {
	seen := map[string]bool{}
	var out []Source
	for _, s := range append(append([]Source{}, a...), b...) {
		if seen[s.URL] {
			continue
		}
		seen[s.URL] = true
		out = append(out, s)
	}
	return out
}

// ---------------------------------------------------------------- OpenAI-compatible chat completions

func openAICall(ctx context.Context, c conn, o callOpts) (*result, error) {
	if c.Base == "" {
		return &result{}, errors.New("Unknown AI provider.")
	}
	msgs := []map[string]any{}
	if o.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": o.System})
	}
	for _, m := range o.Messages {
		role := m.Role
		if role != "assistant" && role != "system" {
			role = "user"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": m.Content})
	}
	body := map[string]any{"model": c.Model, "messages": msgs, "stream": true}
	if c.Provider == "openai" {
		body["max_completion_tokens"] = o.Max
	} else {
		body["max_tokens"] = o.Max
	}
	if c.Provider != "custom" {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if o.Temperature > 0 {
		body["temperature"] = o.Temperature
	}
	if o.JSON && c.Provider == "openai" {
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	h := map[string]string{"Authorization": "Bearer " + c.Key}
	if c.Provider == "openrouter" {
		h["HTTP-Referer"] = c.Referer
		h["X-Title"] = "Setu"
	}
	r := &result{Turns: 1}
	res, err := post(ctx, c, c.Base+"/chat/completions", h, body)
	if err != nil {
		return r, openAIErr(err)
	}
	var text strings.Builder
	var serr error
	err = sse(res, func(d []byte) error {
		var e struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				In  int `json:"prompt_tokens"`
				Out int `json:"completion_tokens"`
			} `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(d, &e) != nil {
			return nil
		}
		if len(e.Error) > 0 && string(e.Error) != "null" {
			m := errMsg(d)
			serr = &apiError{Code: 0, Msg: m, Retryable: reBusy.MatchString(m)}
			return nil
		}
		if len(e.Choices) > 0 {
			t := e.Choices[0].Delta.Content + e.Choices[0].Message.Content
			if t != "" {
				text.WriteString(t)
				if o.OnDelta != nil {
					o.OnDelta(t)
				}
			}
			if e.Choices[0].FinishReason != "" {
				r.Stop = e.Choices[0].FinishReason
			}
		}
		if e.Usage != nil {
			r.In, r.Out = e.Usage.In, e.Usage.Out
		}
		return nil
	})
	if err == nil {
		err = serr
	}
	if err != nil {
		return r, openAIErr(streamErr(ctx, err))
	}
	r.Text = text.String()
	if r.Text == "" {
		return r, &apiError{Code: 200, Msg: "AI returned an empty response."}
	}
	return r, nil
}

func openAIErr(err error) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	out := *ae
	if ae.Code == 0 {
		out.Msg = "The AI did not answer in time (" + ae.Msg + "). Try again or choose a faster model in AI Connection."
		if ae.Retryable {
			out.Msg = "AI error: " + ae.Msg
		}
	} else {
		out.Msg = "AI error " + strconv.Itoa(ae.Code) + ": " + ae.Msg
	}
	return &out
}

// ---------------------------------------------------------------- Google Gemini

func geminiCall(ctx context.Context, c conn, o callOpts) (*result, error) {
	contents := []map[string]any{}
	for _, m := range o.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": []any{map[string]any{"text": m.Content}}})
	}
	gc := map[string]any{"maxOutputTokens": o.Max}
	if o.Temperature > 0 {
		gc["temperature"] = o.Temperature
	}
	if o.JSON {
		gc["responseMimeType"] = "application/json"
	}
	body := map[string]any{"contents": contents, "generationConfig": gc}
	if o.System != "" {
		body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": o.System}}}
	}
	if o.WebSearch {
		body["tools"] = []any{map[string]any{"google_search": map[string]any{}}}
	}
	r := &result{Turns: 1}
	u := c.Base + "/models/" + url.PathEscape(c.Model) + ":streamGenerateContent?alt=sse"
	res, err := post(ctx, c, u, map[string]string{"x-goog-api-key": c.Key}, body)
	var ae *apiError
	if o.WebSearch && errors.As(err, &ae) && ae.Code == 400 {
		delete(body, "tools")
		if o.OnNote != nil {
			o.OnNote("Web search is not available for this model – continuing without it")
		}
		res, err = post(ctx, c, u, map[string]string{"x-goog-api-key": c.Key}, body)
	}
	if err != nil {
		return r, geminiErr(err)
	}
	var text strings.Builder
	var serr error
	err = sse(res, func(d []byte) error {
		// A non-streamed answer is a JSON array of chunks
		if len(d) > 0 && d[0] == '[' {
			var arr []json.RawMessage
			if json.Unmarshal(d, &arr) == nil {
				for _, x := range arr {
					geminiChunk(x, r, &text, o.OnDelta, &serr)
				}
			}
			return nil
		}
		geminiChunk(d, r, &text, o.OnDelta, &serr)
		return nil
	})
	if err == nil {
		err = serr
	}
	if err != nil {
		return r, geminiErr(streamErr(ctx, err))
	}
	r.Text = text.String()
	if r.Text == "" {
		return r, &apiError{Code: 200, Msg: "Gemini returned an empty response."}
	}
	return r, nil
}

func geminiChunk(d []byte, r *result, text *strings.Builder, onDelta func(string), serr *error) {
	var e struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason      string `json:"finishReason"`
			GroundingMetadata struct {
				Chunks []struct {
					Web struct {
						URI   string `json:"uri"`
						Title string `json:"title"`
					} `json:"web"`
				} `json:"groundingChunks"`
			} `json:"groundingMetadata"`
		} `json:"candidates"`
		Usage *struct {
			In       int `json:"promptTokenCount"`
			Out      int `json:"candidatesTokenCount"`
			Thoughts int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(d, &e) != nil {
		return
	}
	if e.Error != nil {
		*serr = &apiError{Code: e.Error.Code, Msg: e.Error.Message, Retryable: reBusy.MatchString(e.Error.Status + " " + e.Error.Message)}
		return
	}
	if len(e.Candidates) > 0 {
		for _, p := range e.Candidates[0].Content.Parts {
			if p.Thought || p.Text == "" {
				continue
			}
			text.WriteString(p.Text)
			if onDelta != nil {
				onDelta(p.Text)
			}
		}
		if e.Candidates[0].FinishReason != "" {
			r.Stop = e.Candidates[0].FinishReason
		}
		var src []Source
		for _, ch := range e.Candidates[0].GroundingMetadata.Chunks {
			if ch.Web.URI != "" {
				src = append(src, Source{Title: ch.Web.Title, URL: ch.Web.URI})
			}
		}
		r.Sources = mergeSources(r.Sources, src)
	}
	if e.Usage != nil {
		r.In = e.Usage.In
		r.Out = e.Usage.Out + e.Usage.Thoughts
	}
}

func geminiErr(err error) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	out := *ae
	if ae.Code == 0 {
		out.Msg = "Gemini did not answer in time (" + ae.Msg + "). Try again or choose a faster model in AI Connection."
		if ae.Retryable {
			out.Msg = "Gemini error: " + ae.Msg
		}
	} else {
		out.Msg = "Gemini error " + strconv.Itoa(ae.Code) + ": " + ae.Msg
	}
	return &out
}

// ---------------------------------------------------------------- plain JSON requests (Message Batches)

func doJSON(ctx context.Context, c conn, method, url string, body any, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range anthropicHeaders(c) {
		req.Header.Set(k, v)
	}
	res, err := clientFor(c).Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s", netMsg(ctx, err))
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 256<<20))
	if err != nil {
		return res.StatusCode, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return res.StatusCode, errors.New(errMsg(raw))
	}
	switch o := out.(type) {
	case nil:
	case *[]byte:
		*o = raw
	default:
		if err := json.Unmarshal(raw, out); err != nil {
			return res.StatusCode, err
		}
	}
	return res.StatusCode, nil
}
