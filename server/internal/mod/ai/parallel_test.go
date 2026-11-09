package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memStore is an in-memory parStore (same rules as the ai_batch queries).
type memStore struct {
	mu      sync.Mutex
	items   []*memItem
	maxRun  int
	beats   int
	stopped []int64
}

type memItem struct {
	parItem
	status string
	nextAt time.Time
	result string
	why    string
	steps  []string
	words  int
}

func (m *memStore) add(n int, prompt func(i int) string) {
	for i := 1; i <= n; i++ {
		m.items = append(m.items, &memItem{parItem: parItem{ID: int64(i), LeadID: int64(100 + i), Req: reqJSON{System: "s", Messages: []Msg{{Role: "user", Content: prompt(i)}}}}, status: "queued"})
	}
}

func (m *memStore) get(id int64) *memItem {
	for _, it := range m.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (m *memStore) claim(_ context.Context, slots int) (*parItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := 0
	for _, it := range m.items {
		if it.status == "running" {
			run++
		}
	}
	if run >= slots {
		return nil, nil
	}
	for _, it := range m.items {
		if it.status == "queued" && !time.Now().Before(it.nextAt) {
			it.status = "running"
			it.Attempts++
			m.maxRun = max(m.maxRun, run+1)
			cp := it.parItem
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memStore) started(_ context.Context, it *parItem, model string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	x := m.get(it.ID)
	x.steps = append(x.steps, "start:"+model)
	return it.ID
}

func (m *memStore) progress(_ context.Context, it *parItem, _ int64, words int, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.get(it.ID).words = max(m.get(it.ID).words, words)
}

func (m *memStore) cancelled(_ context.Context, ids []int64) map[int64]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int64]bool{}
	for _, id := range ids {
		if m.get(id).status == "cancelled" {
			out[id] = true
		}
	}
	return out
}

func (m *memStore) stopped(_ context.Context, it *parItem, _ int64, _ int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = append(m.stopped, it.ID)
}

func (m *memStore) done(_ context.Context, it *parItem, _ int64, _ string, r *result, _ int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x := m.get(it.ID)
	if x.status == "running" {
		x.status, x.result = "done", r.Text
	}
}

func (m *memStore) retry(_ context.Context, it *parItem, _ int64, _ string, _ *result, wait time.Duration, why string, _ int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x := m.get(it.ID)
	x.status, x.nextAt, x.why = "queued", time.Now().Add(wait), why
	x.steps = append(x.steps, "retry")
}

func (m *memStore) fail(_ context.Context, it *parItem, _ int64, _ string, _ *result, why string, _ int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x := m.get(it.ID)
	x.status, x.why = "error", why
}

func (m *memStore) heartbeat(context.Context) {
	m.mu.Lock()
	m.beats++
	m.mu.Unlock()
}

func (m *memStore) count(st string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, it := range m.items {
		if it.status == st {
			n++
		}
	}
	return n
}

var testCfg = parCfg{Slots: 3, Budget: time.Minute, Beat: 10 * time.Millisecond, Timeout: 5 * time.Second, RetryIn: func(int, int) time.Duration { return 20 * time.Millisecond }}

// The runner against a real streaming endpoint: N requests, never more than Slots at once, every answer saved,
// rate limits retried, a bad request given up, a cancelled one stopped.
func TestParallelRunner(t *testing.T) {
	var inFlight, peak atomic.Int32
	var limited sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := capture(r)
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		prompt := c.body["messages"].([]any)[0].(map[string]any)["content"].(string)
		switch {
		case prompt == "bad":
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"prompt is too long"}}`))
			return
		case prompt == "limited":
			if _, seen := limited.LoadOrStore(prompt, true); !seen {
				w.WriteHeader(429)
				w.Write([]byte(`{"error":{"message":"Rate limit reached"}}`))
				return
			}
		case prompt == "slow":
			w.Header().Set("Content-Type", "text/event-stream")
			for i := 0; i < 200; i++ {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
				w.Write([]byte(`data: {"choices":[{"delta":{"content":"word word "}}]}` + "\n\n"))
				w.(http.Flusher).Flush()
			}
			return
		}
		time.Sleep(30 * time.Millisecond)
		openAIStream(w, `{"choices":[{"delta":{"content":"answer to `+prompt+`"}}]}`, `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3}}`)
	}))
	defer srv.Close()

	st := &memStore{}
	st.add(12, func(i int) string {
		switch i {
		case 4:
			return "bad"
		case 7:
			return "limited"
		case 9:
			return "slow"
		}
		return "p" + strconv.Itoa(i)
	})
	// the user aborts the slow one while it is being written
	go func() {
		time.Sleep(150 * time.Millisecond)
		st.mu.Lock()
		st.get(9).status = "cancelled"
		st.mu.Unlock()
	}()
	n := runParallel(context.Background(), st, conn{Provider: "openai", Key: "k", Model: "gpt-4.1", Base: srv.URL}, testCfg, call)

	if st.maxRun > 3 || peak.Load() > 3 {
		t.Fatalf("more than 3 at once: store %d server %d", st.maxRun, peak.Load())
	}
	if peak.Load() < 2 {
		t.Fatalf("requests did not run in parallel (peak %d)", peak.Load())
	}
	if got := st.count("done"); got != 10 {
		t.Fatalf("done %d, want 10 (n=%d)", got, n)
	}
	for _, it := range st.items {
		if it.status == "done" && it.result != "answer to "+it.Req.Messages[0].Content {
			t.Fatalf("item %d saved %q", it.ID, it.result)
		}
	}
	if bad := st.get(4); bad.status != "error" || !strings.Contains(bad.why, "prompt is too long") || bad.Attempts != 1 {
		t.Fatalf("bad request: %+v", bad)
	}
	if lim := st.get(7); lim.status != "done" || lim.Attempts != 2 || !strings.Contains(lim.why, "429") {
		t.Fatalf("rate-limited request: %+v", lim)
	}
	if slow := st.get(9); slow.status != "cancelled" || len(st.stopped) != 1 || st.stopped[0] != 9 || slow.words == 0 {
		t.Fatalf("cancelled request: %+v stopped %v", slow, st.stopped)
	}
	if st.beats == 0 {
		t.Fatal("no heartbeat")
	}
}

// Retries stop after 3 attempts; the request is then given up (the caller writes it directly).
func TestParallelRunnerGivesUp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(529)
		w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer srv.Close()
	st := &memStore{}
	st.add(1, func(int) string { return "x" })
	runParallel(context.Background(), st, conn{Provider: "anthropic", Key: "k", Model: "claude-haiku-5-5", Base: srv.URL}, testCfg, call)
	it := st.get(1)
	if it.status != "error" || it.Attempts != 3 || calls.Load() != 3 || !strings.Contains(it.why, "Overloaded") {
		t.Fatalf("%+v calls %d", it, calls.Load())
	}
}

// Items waiting for a retry slot are left in the queue when the budget for new requests is used up.
func TestParallelRunnerBudget(t *testing.T) {
	st := &memStore{}
	st.add(5, func(i int) string { return "p" })
	cfg := testCfg
	cfg.Budget = 0
	n := runParallel(context.Background(), st, conn{Provider: "openai"}, cfg, func(ctx context.Context, c conn, o callOpts) (*result, error) {
		return &result{Text: "x"}, nil
	})
	if n != 0 || st.count("queued") != 5 {
		t.Fatalf("budget 0 still started %d", n)
	}
	cfg.Budget = time.Minute
	cfg.Slots = 2
	n = runParallel(context.Background(), st, conn{Provider: "openai", Model: "m"}, cfg, func(ctx context.Context, c conn, o callOpts) (*result, error) {
		o.OnDelta("xxxxxx")
		return &result{Text: "x", In: 1, Out: 1}, nil
	})
	if n != 5 || st.count("done") != 5 || st.maxRun > 2 {
		t.Fatalf("n %d done %d maxRun %d", n, st.count("done"), st.maxRun)
	}
}
