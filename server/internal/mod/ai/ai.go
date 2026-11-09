// Package ai: AI providers (Claude, ChatGPT, OpenRouter, Gemini, Groq, DeepSeek, custom), usage & cost,
// the bulk engine (parallel requests and Claude Message Batches), the process log and the activity drawer.
// OWNER: ai-engine module. The exported API below is a contract used by other modules – keep it.
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

type Msg struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
}

type Request struct {
	System      string
	Messages    []Msg
	MaxTokens   int
	Temperature float64
	Model       string // optional override of the workspace model
	WebSearch   bool   // live web research (Claude web search tool) when the provider supports it
	JSON        bool   // ask for a JSON answer
	Purpose     string // usage op label, e.g. "report", "audit", "research"
	Trace       *shared.Trace
	OnDelta     func(text string) // streaming progress (optional)

	Provider string        // optional provider override (e.g. the "Test connection" button)
	Label    string        // step name in the process log (default: Purpose)
	Searches int           // max web searches (default 4)
	Deadline time.Duration // whole call incl. retries (default 280s, like v1)
}

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Response struct {
	Text      string
	Model     string
	TokensIn  int
	TokensOut int
	Cost      float64
	Sources   []Source

	Provider    string
	WebSearches int
	Duration    time.Duration
}

// Complete sends one request with the workspace's AI connection, records usage and the process step.
// It streams the answer, shows live progress on the lead (when r.Trace is set), retries a busy provider
// and stops at once when the user aborts the lead (returns shared.ErrAborted).
func Complete(ctx context.Context, a *app.App, orgID int64, s *settings.Store, r Request) (*Response, error) {
	c, err := connFor(s, r.Provider, a.Cfg.AppURL())
	if err != nil {
		return nil, err
	}
	if r.Model != "" {
		c.Model = r.Model
	}
	label := r.Label
	if label == "" {
		label = strings.ToUpper(r.Purpose[:min(1, len(r.Purpose))]) + r.Purpose[min(1, len(r.Purpose)):]
		if label == "" {
			label = "AI request"
		}
	}
	op := r.Purpose
	if op == "" {
		op = "chat"
	}
	tr := r.Trace
	if tr != nil && tr.Lead != 0 {
		tr.Step(ctx, label, "ai")
		tr.AI(ctx, cut(c.Model, 90), 0, 0, 0)
	}
	dl := r.Deadline
	if dl <= 0 {
		dl = 280 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, dl)
	defer cancel()

	// Heartbeat: live progress on the lead and in the process log, job kept alive, abort checked
	var chars atomic.Int64
	var aborted atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup
	t0 := time.Now()
	if tr != nil && tr.Lead != 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk := time.NewTicker(4 * time.Second)
			defer tk.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tk.C:
				}
				el := time.Since(t0)
				w := int(chars.Load() / 6)
				detail := "Waiting for the AI to answer · " + elapsed(el)
				if el > 120*time.Second {
					detail += " – slower than usual; the provider may be busy"
				}
				msg := label + " – waiting for " + c.Model + " · " + elapsed(el)
				if w > 0 {
					detail = shared.FormatNum(float64(w)) + " words written so far · " + elapsed(el)
					msg = label + " · " + shared.FormatNum(float64(w)) + " words · " + elapsed(el)
				}
				if beat(ctx, a, orgID, tr.Lead, tr.Job, msg) {
					aborted.Store(true)
					cancel()
					return
				}
				tr.Note(ctx, detail)
			}
		}()
	}
	onDelta := func(t string) {
		chars.Add(int64(utf8.RuneCountInString(t)))
		if r.OnDelta != nil {
			r.OnDelta(t)
		}
	}
	res, err := call(cctx, c, callOpts{System: r.System, Messages: r.Messages, Max: r.MaxTokens, Temperature: r.Temperature, JSON: r.JSON,
		WebSearch: r.WebSearch, Searches: r.Searches, OnDelta: onDelta,
		OnNote: func(d string) {
			if tr != nil {
				tr.Note(ctx, d)
			}
		}})
	close(stop)
	wg.Wait()
	if res == nil {
		res = &result{}
	}
	cost := Cost(s, c.Model, res.In, res.Out, 1)
	failed := err != nil || strings.TrimSpace(res.Text) == ""
	bg := context.WithoutCancel(ctx)
	a.DB.Org(bg, orgID, func(tx pgx.Tx) error {
		app.TrackUsage(bg, tx, c.Provider, op, int64(res.In), int64(res.Out), cost, failed)
		if res.WebSearches > 0 {
			app.TrackUsage(bg, tx, "web_search", op, int64(res.WebSearches), 0, searchCost(s, res.WebSearches), false)
		}
		return nil
	})
	total := cost + searchCost(s, res.WebSearches)
	took := time.Since(t0)
	if tr != nil && tr.Lead != 0 {
		tr.AI(ctx, cut(c.Model, 90), res.In, res.Out, round5(total))
	}
	if aborted.Load() || errors.Is(err, shared.ErrAborted) {
		tr.Close(ctx, "err", "Stopped by user")
		return nil, shared.ErrAborted
	}
	if err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil && !strings.Contains(err.Error(), "in time") {
			err = fmt.Errorf("The AI did not answer in time (timeout). Try again or choose a faster model in AI Connection.")
		}
		tr.Close(ctx, "err", err.Error())
		return nil, err
	}
	detail := fmt.Sprintf("Answered in %.1fs · %s characters", took.Seconds(), shared.FormatNum(float64(utf8.RuneCountInString(res.Text))))
	if res.WebSearches > 0 {
		detail += fmt.Sprintf(" · %d web search%s", res.WebSearches, map[bool]string{true: "es"}[res.WebSearches > 1])
	}
	if res.Turns > 1 {
		detail += fmt.Sprintf(" · %d requests", res.Turns)
	}
	tr.Close(ctx, "ok", detail)
	return &Response{Text: res.Text, Model: c.Model, TokensIn: res.In, TokensOut: res.Out, Cost: total, Sources: res.Sources,
		Provider: c.Provider, WebSearches: res.WebSearches, Duration: took}, nil
}

// beat updates the lead's progress message and keeps the job alive; true when the user pressed Abort.
func beat(ctx context.Context, a *app.App, orgID, leadID, jobID int64, msg string) bool {
	var abort bool
	bg := context.WithoutCancel(ctx)
	a.DB.Org(bg, orgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(bg, `UPDATE leads SET progress_msg=$2 WHERE id=$1 RETURNING abort_flag`, leadID, cut(msg, 180)).Scan(&abort); err != nil {
			return err
		}
		if jobID > 0 {
			tx.Exec(bg, `UPDATE jobs SET locked_at=now() WHERE id=$1 AND status='running'`, jobID)
		}
		return nil
	})
	return abort
}

func elapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}

func round5(f float64) float64 { return float64(int64(f*100000+0.5)) / 100000 }

// Configured reports whether the workspace has an AI connection.
func Configured(s *settings.Store) bool {
	if s == nil {
		return false
	}
	_, err := connFor(s, "", "")
	return err == nil
}

// ResearchOn reports whether the market research step for reports is switched on (default on).
func ResearchOn(s *settings.Store) bool { return s.Get(SetResearch, "1") == "1" }

// BulkMode is how a bulk run writes its AI requests (v1 Batch::mode): "batch" (Claude Message Batches,
// half price), "parallel" (any provider, several at once) or "" (one by one).
func BulkMode(s *settings.Store) string {
	m := s.Get(SetBulk, "parallel")
	if m == "off" {
		return ""
	}
	if m == "batch" && ActiveProvider(s) == "anthropic" {
		return "batch"
	}
	return "parallel"
}

// Slots is how many bulk requests run at the same time (setting ai_parallel, 1–10, default 5).
func Slots(s *settings.Store) int { return max(1, min(10, s.Int(SetParallel, 5))) }

func Register(a *app.App) {
	// AI connection page
	a.Get("/ai", getConn, app.Perm("settings"))
	a.Put("/ai", saveConn, app.Perm("settings"))
	a.Post("/ai/test", testConn, app.Perm("settings"))
	// Process log & activity
	a.Get("/leads/{id}/process", leadProcess, app.Perm("leads"))
	a.Get("/activity", activity, app.Perm("settings"))
	a.Get("/activity/tick", tick, app.Perm("settings"))
	a.Get("/activity/events", events, app.Perm("dashboard"))
	a.Post("/activity/stop-all", stopAll, app.Perm("settings"))
	a.Post("/activity/abort", abortLeads, app.Perm("settings"))
	a.Post("/activity/unschedule", unschedule, app.Perm("settings"))
	a.Post("/activity/cancel-run", cancelRun, app.Perm("settings"))
	// Bulk engine
	a.Jobs.Handle(JobParallel, 15*time.Minute, func(ctx context.Context, j *jobsJob) error { return runParallelJob(ctx, a, j) })
	a.Jobs.Handle(JobBatch, 5*time.Minute, func(ctx context.Context, j *jobsJob) error { return runBatchJob(ctx, a, j) })
	a.Jobs.Every("ai-bulk", time.Minute, func(ctx context.Context) error { return sweep(ctx, a) })
}
