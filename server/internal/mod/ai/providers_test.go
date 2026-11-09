package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func init() { retryMin, retryBase = time.Millisecond, 5*time.Millisecond }

// sseWrite writes Server-Sent Events (Anthropic style: event + data).
func sseWrite(w http.ResponseWriter, events ...any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		b, _ := json.Marshal(e)
		typ := ""
		if m, ok := e.(map[string]any); ok {
			typ, _ = m["type"].(string)
		}
		if typ != "" {
			fmt.Fprintf(w, "event: %s\n", typ)
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

type captured struct {
	path   string
	header http.Header
	body   map[string]any
}

func capture(r *http.Request) captured {
	var b map[string]any
	raw, _ := io.ReadAll(r.Body)
	json.Unmarshal(raw, &b)
	return captured{path: r.URL.RequestURI(), header: r.Header.Clone(), body: b}
}

func ev(typ string, kv ...any) map[string]any {
	m := map[string]any{"type": typ}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func textDelta(i int, t string) map[string]any {
	return ev("content_block_delta", "index", i, "delta", map[string]any{"type": "text_delta", "text": t})
}

func TestAnthropicStreamWithWebSearch(t *testing.T) {
	var got captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = capture(r)
		sseWrite(w,
			ev("message_start", "message", map[string]any{"usage": map[string]any{"input_tokens": 100, "cache_read_input_tokens": 20, "output_tokens": 1}}),
			ev("content_block_start", "index", 0, "content_block", map[string]any{"type": "server_tool_use", "id": "srv_1", "name": "web_search", "input": map[string]any{}}),
			ev("content_block_delta", "index", 0, "delta", map[string]any{"type": "input_json_delta", "partial_json": `{"query":"dentists`}),
			ev("content_block_delta", "index", 0, "delta", map[string]any{"type": "input_json_delta", "partial_json": ` pune"}`}),
			ev("content_block_stop", "index", 0),
			ev("content_block_start", "index", 1, "content_block", map[string]any{"type": "web_search_tool_result", "tool_use_id": "srv_1", "content": []any{
				map[string]any{"type": "web_search_result", "url": "https://a.example/x", "title": "A"},
				map[string]any{"type": "web_search_result", "url": "https://b.example/y", "title": "B"}}}),
			ev("content_block_stop", "index", 1),
			ev("content_block_start", "index", 2, "content_block", map[string]any{"type": "text", "text": ""}),
			textDelta(2, "Hello "),
			ev("content_block_delta", "index", 2, "delta", map[string]any{"type": "citations_delta", "citation": map[string]any{"type": "web_search_result_location", "url": "https://b.example/y", "title": "B"}}),
			textDelta(2, "world"),
			ev("content_block_stop", "index", 2),
			ev("message_delta", "delta", map[string]any{"stop_reason": "end_turn"}, "usage", map[string]any{"output_tokens": 42, "server_tool_use": map[string]any{"web_search_requests": 1}}),
			ev("message_stop"),
		)
	}))
	defer srv.Close()
	var streamed strings.Builder
	r, err := call(context.Background(), conn{Provider: "anthropic", Key: "sk-ant-x", Model: "claude-sonnet-5-5", Base: srv.URL},
		callOpts{System: "sys", Messages: []Msg{{Role: "user", Content: "hi"}}, Max: 1000, WebSearch: true, Searches: 3, OnDelta: func(s string) { streamed.WriteString(s) }})
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/v1/messages" || got.header.Get("x-api-key") != "sk-ant-x" || got.header.Get("anthropic-version") != anthropicVersion {
		t.Fatalf("bad request %v %v", got.path, got.header)
	}
	if got.body["stream"] != true || got.body["model"] != "claude-sonnet-5-5" || got.body["system"] != "sys" || got.body["max_tokens"].(float64) != 1000 {
		t.Fatalf("bad body %v", got.body)
	}
	if oc, _ := got.body["output_config"].(map[string]any); oc["effort"] != "medium" {
		t.Fatalf("effort missing: %v", got.body)
	}
	tools, _ := got.body["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search_20260209" || tools[0].(map[string]any)["max_uses"].(float64) != 3 {
		t.Fatalf("bad tools %v", got.body["tools"])
	}
	if got.body["fallbacks"] != "default" || got.header.Get("anthropic-beta") != "server-side-fallback-2026-07-01" {
		t.Fatal("claude-sonnet-5-5 asks for the server-side refusal fallback")
	}
	if r.Text != "Hello world" || streamed.String() != "Hello world" {
		t.Fatalf("text %q streamed %q", r.Text, streamed.String())
	}
	if r.In != 120 || r.Out != 42 || r.WebSearches != 1 {
		t.Fatalf("usage %+v", r)
	}
	if len(r.Sources) != 2 || r.Sources[0].URL != "https://b.example/y" || r.Sources[1].URL != "https://a.example/x" {
		t.Fatalf("sources %+v", r.Sources)
	}
}

func TestAnthropicFallbackAndToolsRetry(t *testing.T) {
	var n atomic.Int32
	var bodies []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := capture(r)
		bodies = append(bodies, c)
		switch n.Add(1) {
		case 1: // fallback not available
			w.WriteHeader(400)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"fallbacks: unknown field"}}`))
		case 2: // web search not available
			w.WriteHeader(400)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"tools not supported"}}`))
		default:
			sseWrite(w, ev("message_start", "message", map[string]any{"usage": map[string]any{"input_tokens": 5}}),
				ev("content_block_start", "index", 0, "content_block", map[string]any{"type": "text", "text": ""}),
				textDelta(0, "ok"), ev("content_block_stop", "index", 0),
				ev("message_delta", "delta", map[string]any{"stop_reason": "end_turn"}, "usage", map[string]any{"output_tokens": 2}))
		}
	}))
	defer srv.Close()
	var notes []string
	r, err := call(context.Background(), conn{Provider: "anthropic", Key: "k", Model: "claude-opus-5-5", Base: srv.URL},
		callOpts{System: "s", Messages: []Msg{{Role: "user", Content: "x"}}, WebSearch: true, OnNote: func(s string) { notes = append(notes, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "ok" || len(bodies) != 3 {
		t.Fatalf("text %q calls %d", r.Text, len(bodies))
	}
	if bodies[0].body["fallbacks"] != "default" || bodies[0].header.Get("anthropic-beta") != "server-side-fallback-2026-07-01" {
		t.Fatalf("first request should ask for the fallback: %v", bodies[0].body)
	}
	if _, ok := bodies[1].body["fallbacks"]; ok {
		t.Fatal("second request must drop fallbacks")
	}
	if _, ok := bodies[2].body["tools"]; ok {
		t.Fatal("third request must drop tools")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "Web search is not available") {
		t.Fatalf("notes %v", notes)
	}
}

func TestAnthropicPauseTurn(t *testing.T) {
	var n atomic.Int32
	var second captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := capture(r)
		if n.Add(1) == 1 {
			sseWrite(w, ev("message_start", "message", map[string]any{"usage": map[string]any{"input_tokens": 10}}),
				ev("content_block_start", "index", 0, "content_block", map[string]any{"type": "text", "text": ""}),
				textDelta(0, "Part one. "), ev("content_block_stop", "index", 0),
				ev("message_delta", "delta", map[string]any{"stop_reason": "pause_turn"}, "usage", map[string]any{"output_tokens": 3}))
			return
		}
		second = c
		sseWrite(w, ev("message_start", "message", map[string]any{"usage": map[string]any{"input_tokens": 20}}),
			ev("content_block_start", "index", 0, "content_block", map[string]any{"type": "text", "text": ""}),
			textDelta(0, "Part two."), ev("content_block_stop", "index", 0),
			ev("message_delta", "delta", map[string]any{"stop_reason": "end_turn"}, "usage", map[string]any{"output_tokens": 4}))
	}))
	defer srv.Close()
	r, err := call(context.Background(), conn{Provider: "anthropic", Key: "k", Model: "claude-haiku-5-5", Base: srv.URL}, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "Part one. Part two." || r.In != 30 || r.Out != 7 || r.Turns != 2 {
		t.Fatalf("%+v", r)
	}
	msgs := second.body["messages"].([]any)
	if len(msgs) != 2 || msgs[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("continuation messages %v", msgs)
	}
	blk := msgs[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if blk["type"] != "text" || blk["text"] != "Part one. " {
		t.Fatalf("assistant content %v", blk)
	}
}

func TestAnthropicRefusalAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "bad" {
			w.WriteHeader(401)
			w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		sseWrite(w, ev("message_start", "message", map[string]any{"usage": map[string]any{"input_tokens": 1}}),
			ev("message_delta", "delta", map[string]any{"stop_reason": "refusal", "stop_details": map[string]any{"category": "cyber"}}, "usage", map[string]any{"output_tokens": 1}))
	}))
	defer srv.Close()
	_, err := call(context.Background(), conn{Provider: "anthropic", Key: "k", Model: "claude-haiku-5-5", Base: srv.URL}, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}})
	if err == nil || err.Error() != "Claude declined this request (cyber). Try again or edit the prompt." {
		t.Fatalf("refusal: %v", err)
	}
	_, err = call(context.Background(), conn{Provider: "anthropic", Key: "bad", Model: "claude-haiku-5-5", Base: srv.URL}, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}})
	if err == nil || err.Error() != "Claude API error 401: invalid x-api-key" || retryable(err) {
		t.Fatalf("401: %v", err)
	}
}

func TestRetryOnRateLimitAndOverload(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) {
		case 1:
			w.WriteHeader(429)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
		case 2: // overloaded in the middle of the stream
			sseWrite(w, ev("message_start", "message", map[string]any{"usage": map[string]any{"input_tokens": 1}}),
				ev("error", "error", map[string]any{"type": "overloaded_error", "message": "Overloaded"}))
		default:
			sseWrite(w, ev("content_block_start", "index", 0, "content_block", map[string]any{"type": "text", "text": "fine"}),
				ev("message_delta", "delta", map[string]any{"stop_reason": "end_turn"}, "usage", map[string]any{"output_tokens": 1}))
		}
	}))
	defer srv.Close()
	var notes int
	r, err := call(context.Background(), conn{Provider: "anthropic", Key: "k", Model: "claude-haiku-5-5", Base: srv.URL},
		callOpts{Messages: []Msg{{Role: "user", Content: "x"}}, OnNote: func(string) { notes++ }})
	if err != nil || r.Text != "fine" || notes != 2 || n.Load() != 3 {
		t.Fatalf("err %v text %q notes %d calls %d", err, r.Text, notes, n.Load())
	}
	// The bulk engine retries itself: NoRetry hands back the retryable error at once
	n.Store(0)
	_, err = call(context.Background(), conn{Provider: "anthropic", Key: "k", Model: "claude-haiku-5-5", Base: srv.URL}, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}, NoRetry: true})
	var ae *apiError
	if !errors.As(err, &ae) || ae.Code != 429 || !retryable(err) || n.Load() != 1 {
		t.Fatalf("no-retry: %v", err)
	}
}

func openAIStream(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestOpenAICompatibleProviders(t *testing.T) {
	for _, pv := range []string{"openai", "openrouter", "groq", "deepseek", "custom"} {
		t.Run(pv, func(t *testing.T) {
			var got captured
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = capture(r)
				openAIStream(w,
					`{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
					`{"choices":[{"delta":{"content":"Connection"}}]}`,
					`{"choices":[{"delta":{"content":" OK"},"finish_reason":"stop"}]}`,
					`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`)
			}))
			defer srv.Close()
			var d strings.Builder
			r, err := call(context.Background(), conn{Provider: pv, Key: "sk-1", Model: "m-1", Base: srv.URL, Referer: "https://app.example"},
				callOpts{System: "sys", Messages: []Msg{{Role: "user", Content: "hi"}}, Max: 200, JSON: pv == "openai", OnDelta: func(s string) { d.WriteString(s) }})
			if err != nil {
				t.Fatal(err)
			}
			if r.Text != "Connection OK" || d.String() != "Connection OK" || r.In != 12 || r.Out != 3 || r.Stop != "stop" {
				t.Fatalf("%+v", r)
			}
			if got.path != "/chat/completions" || got.header.Get("Authorization") != "Bearer sk-1" || got.body["stream"] != true {
				t.Fatalf("request %v %v", got.path, got.header)
			}
			msgs := got.body["messages"].([]any)
			if msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["content"] != "hi" {
				t.Fatalf("messages %v", msgs)
			}
			maxKey := "max_tokens"
			if pv == "openai" {
				maxKey = "max_completion_tokens"
				if rf, _ := got.body["response_format"].(map[string]any); rf["type"] != "json_object" {
					t.Fatalf("json mode %v", got.body)
				}
			}
			if got.body[maxKey].(float64) != 200 {
				t.Fatalf("%s missing: %v", maxKey, got.body)
			}
			if _, ok := got.body["stream_options"]; ok == (pv == "custom") {
				t.Fatalf("stream_options for %s: %v", pv, got.body)
			}
			if pv == "openrouter" && (got.header.Get("HTTP-Referer") != "https://app.example" || got.header.Get("X-Title") != "Setu") {
				t.Fatalf("openrouter headers %v", got.header)
			}
		})
	}
	if openAIBases["groq"] != "https://api.groq.com/openai/v1" || openAIBases["deepseek"] != "https://api.deepseek.com/v1" || openAIBases["openrouter"] != "https://openrouter.ai/api/v1" {
		t.Fatal("bases changed")
	}
}

func TestOpenAINonStreamingAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer bad" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
			return
		}
		if r.Header.Get("Authorization") == "Bearer empty" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
			return
		}
		// an endpoint that ignores "stream"
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"plain answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`))
	}))
	defer srv.Close()
	c := conn{Provider: "custom", Key: "k", Model: "m", Base: srv.URL}
	r, err := call(context.Background(), c, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}})
	if err != nil || r.Text != "plain answer" || r.In != 7 || r.Out != 2 {
		t.Fatalf("%v %+v", err, r)
	}
	c.Key = "bad"
	if _, err := call(context.Background(), c, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}}); err == nil || err.Error() != "AI error 401: Incorrect API key provided" {
		t.Fatalf("401: %v", err)
	}
	c.Key = "empty"
	if _, err := call(context.Background(), c, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}}); err == nil || err.Error() != "AI returned an empty response." {
		t.Fatalf("empty: %v", err)
	}
}

func TestTimeoutMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := call(ctx, conn{Provider: "openai", Key: "k", Model: "m", Base: srv.URL}, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "did not answer in time (timeout)") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestGemini(t *testing.T) {
	var got captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = capture(r)
		if r.Header.Get("x-goog-api-key") == "bad" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}`))
			return
		}
		openAIStream(w,
			`{"candidates":[{"content":{"parts":[{"text":"thinking…","thought":true}],"role":"model"}}]}`,
			`{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}`,
			`{"candidates":[{"content":{"parts":[{"text":" there"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"thoughtsTokenCount":6}}`)
	}))
	defer srv.Close()
	c := conn{Provider: "gemini", Key: "g-key", Model: "gemini-2.5-flash", Base: srv.URL}
	r, err := call(context.Background(), c, callOpts{System: "sys", Messages: []Msg{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "yo"}}, Max: 300})
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/models/gemini-2.5-flash:streamGenerateContent?alt=sse" || got.header.Get("x-goog-api-key") != "g-key" {
		t.Fatalf("request %s %v", got.path, got.header)
	}
	si := got.body["systemInstruction"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"]
	contents := got.body["contents"].([]any)
	if si != "sys" || len(contents) != 2 || contents[1].(map[string]any)["role"] != "model" || got.body["generationConfig"].(map[string]any)["maxOutputTokens"].(float64) != 300 {
		t.Fatalf("body %v", got.body)
	}
	if r.Text != "Hello there" || r.In != 9 || r.Out != 10 || r.Stop != "STOP" {
		t.Fatalf("%+v", r)
	}
	c.Key = "bad"
	if _, err := call(context.Background(), c, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}}); err == nil || err.Error() != "Gemini error 400: API key not valid" {
		t.Fatalf("400: %v", err)
	}
}

func TestBatchResultsAndParams(t *testing.T) {
	raw := []byte(`{"custom_id":"r1","result":{"type":"succeeded","message":{"content":[{"type":"text","text":"{\"a\":1}"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":5,"output_tokens":7}}}}
{"custom_id":"r2","result":{"type":"errored","error":{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}}}
{"custom_id":"r3","result":{"type":"expired"}}
`)
	by := parseBatchResults(raw)
	if a := by["r1"]; a.Type != "succeeded" || a.Text != `{"a":1}` || a.In != 15 || a.Out != 7 {
		t.Fatalf("r1 %+v", a)
	}
	if a := by["r2"]; a.Type != "errored" || a.Error != "Overloaded" {
		t.Fatalf("r2 %+v", a)
	}
	if by["r3"].Type != "expired" {
		t.Fatal("r3")
	}
	p := batchParams(reqJSON{System: "s", Messages: []Msg{{Role: "user", Content: "u"}}, MaxTokens: 16000, WebSearch: true}, "claude-opus-5-5")
	if p["model"] != "claude-opus-5-5" || p["max_tokens"] != 16000 || p["output_config"] == nil || p["tools"] == nil || p["stream"] != nil {
		t.Fatalf("params %v", p)
	}
}

func TestBatchSendAndPollHTTP(t *testing.T) {
	var sent captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/messages/batches":
			sent = capture(r)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"msgbatch_1","processing_status":"in_progress"}`))
		case r.URL.Path == "/v1/messages/batches/msgbatch_1":
			w.Write([]byte(`{"id":"msgbatch_1","processing_status":"ended","request_counts":{"succeeded":1,"errored":0},"results_url":"` + "http://" + r.Host + `/v1/messages/batches/msgbatch_1/results"}`))
		case r.URL.Path == "/v1/messages/batches/msgbatch_1/results":
			w.Write([]byte(`{"custom_id":"r9","result":{"type":"succeeded","message":{"content":[{"type":"text","text":"done"}],"usage":{"input_tokens":1,"output_tokens":1}}}}` + "\n"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := conn{Provider: "anthropic", Key: "k", Model: "claude-haiku-5-5", Base: srv.URL}
	var out struct {
		ID string `json:"id"`
	}
	if _, err := doJSON(context.Background(), c, "POST", c.Base+"/v1/messages/batches", map[string]any{"requests": []any{map[string]any{"custom_id": "r9", "params": batchParams(reqJSON{Messages: []Msg{{Role: "user", Content: "x"}}}, c.Model)}}}, &out); err != nil || out.ID != "msgbatch_1" {
		t.Fatalf("send %v %v", err, out)
	}
	if sent.header.Get("x-api-key") != "k" || sent.body["requests"] == nil {
		t.Fatalf("sent %v", sent)
	}
	var st batchStatus
	if _, err := doJSON(context.Background(), c, "GET", c.Base+"/v1/messages/batches/msgbatch_1", nil, &st); err != nil || st.ProcessingStatus != "ended" || st.Counts.Succeeded != 1 {
		t.Fatalf("poll %v %+v", err, st)
	}
	var raw []byte
	if _, err := doJSON(context.Background(), c, "GET", st.ResultsURL, nil, &raw); err != nil || parseBatchResults(raw)["r9"].Text != "done" {
		t.Fatalf("results %v %s", err, raw)
	}
}

func TestPricesAndHelpers(t *testing.T) {
	if c := Cost(nil, "claude-sonnet-5-5", 1_000_000, 100_000, 1); c < 2.999 || c > 3.001 {
		t.Fatalf("cost %v", c)
	}
	if c := Cost(nil, "anthropic/claude-sonnet-5-5", 1_000_000, 0, 0.5); c < 0.999 || c > 1.001 {
		t.Fatalf("openrouter id / batch half price: %v", c)
	}
	if Cost(nil, "unknown-model", 1000, 1000, 1) != 0 {
		t.Fatal("unknown model has no price")
	}
	if JobTitle("report", []byte(`{"phase":"write"}`), 2) != "Report · step 2 of 3: AI writes the report (attempt 2)" || JobTitle("audit", nil, 1) != "Audit started" {
		t.Fatal("titles")
	}
	if errMsg([]byte(`[{"error":{"message":"x"}}]`)) != "x" || errMsg([]byte(`<html><b>Bad gateway</b></html>`)) != "Bad gateway" {
		t.Fatal("errMsg")
	}
	if webSearchTool("claude-haiku-5-5", 0)["type"] != "web_search_20250305" || webSearchTool("claude-opus-4-7", 2)["type"] != "web_search_20260209" {
		t.Fatal("web search tool version")
	}
	if (reqJSON{}).working() != "AI is writing the report" {
		t.Fatal("working msg")
	}
}

func TestGuardedClientBlocksPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	defer srv.Close()
	_, err := call(context.Background(), conn{Provider: "custom", Key: "k", Model: "m", Base: srv.URL, Guard: true}, callOpts{Messages: []Msg{{Role: "user", Content: "x"}}, NoRetry: true})
	if err == nil || !strings.Contains(err.Error(), "not a public web address") {
		t.Fatalf("guard: %v", err)
	}
}
