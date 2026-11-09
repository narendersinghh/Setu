// Package jobs is a PostgreSQL-backed job queue (FOR UPDATE SKIP LOCKED) with per-organisation
// fairness, retries with backoff, deferral, cancellation and periodic tasks.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/narendersinghh/setu/server/internal/db"
)

type Job struct {
	ID          int64           `db:"id"`
	OrgID       int64           `db:"org_id"`
	Type        string          `db:"type"`
	RefID       int64           `db:"ref_id"`
	Payload     json.RawMessage `db:"payload"`
	Attempts    int16           `db:"attempts"`
	MaxAttempts int16           `db:"max_attempts"`
}

// Decode unmarshals the payload.
func (j *Job) Decode(v any) error { return json.Unmarshal(j.Payload, v) }

type Handler func(ctx context.Context, j *Job) error

// DeferError asks the queue to run the job again later without counting an attempt.
type DeferError struct{ After time.Duration }

func (d DeferError) Error() string { return "deferred " + d.After.String() }

func Defer(d time.Duration) error { return DeferError{After: d} }

// ErrCancelled stops a job without retry (e.g. aborted by the user).
var ErrCancelled = errors.New("cancelled")

// Permanent wraps an error that must not be retried.
type PermanentError struct{ Err error }

func (p PermanentError) Error() string { return p.Err.Error() }
func (p PermanentError) Unwrap() error { return p.Err }
func Permanent(err error) error        { return PermanentError{Err: err} }

type typeCfg struct {
	h       Handler
	timeout time.Duration
}

type periodic struct {
	name  string
	every time.Duration
	fn    func(ctx context.Context) error
}

type Queue struct {
	db       *db.DB
	mu       sync.RWMutex
	types    map[string]typeCfg
	periodic []periodic
	orgCap   int
	id       string
}

func New(d *db.DB, orgCap int) *Queue {
	h, _ := os.Hostname()
	return &Queue{db: d, types: map[string]typeCfg{}, orgCap: max(1, orgCap), id: h + ":" + strconv.Itoa(os.Getpid())}
}

// Handle registers the handler of a job type. timeout 0 = 10 minutes.
func (q *Queue) Handle(typ string, timeout time.Duration, h Handler) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	q.mu.Lock()
	q.types[typ] = typeCfg{h: h, timeout: timeout}
	q.mu.Unlock()
}

// Every registers a periodic task (runs on exactly one worker per tick across all servers).
func (q *Queue) Every(name string, every time.Duration, fn func(ctx context.Context) error) {
	q.periodic = append(q.periodic, periodic{name: name, every: every, fn: fn})
}

type Opt struct {
	At          time.Time
	Priority    int
	MaxAttempts int
}

// Push enqueues a job. Pass the caller's tenant transaction so the job commits with the data.
func Push(ctx context.Context, x db.Q, orgID int64, typ string, ref int64, payload any, o ...Opt) (int64, error) {
	p := []byte("{}")
	if payload != nil {
		var err error
		if p, err = json.Marshal(payload); err != nil {
			return 0, err
		}
	}
	var op Opt
	if len(o) > 0 {
		op = o[0]
	}
	at := op.At
	if at.IsZero() {
		at = time.Now()
	}
	ma := op.MaxAttempts
	if ma <= 0 {
		ma = 3
	}
	var id int64
	err := x.QueryRow(ctx, `INSERT INTO jobs (org_id,type,ref_id,payload,run_after,priority,max_attempts) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		orgID, typ, ref, p, at, op.Priority, ma).Scan(&id)
	return id, err
}

// Run starts the dispatcher with n concurrent workers until ctx is cancelled.
func (q *Queue) Run(ctx context.Context, n int) {
	slog.Info("worker started", "workers", n, "id", q.id)
	go q.scheduler(ctx)
	go q.reaper(ctx)
	sem := make(chan struct{}, max(1, n))
	var wg sync.WaitGroup
	idle := 200 * time.Millisecond
	for ctx.Err() == nil {
		sem <- struct{}{}
		j, err := q.claim(ctx)
		if err != nil || j == nil {
			<-sem
			if err != nil && ctx.Err() == nil {
				slog.Error("claim", "err", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(idle):
			}
			if idle < 2*time.Second {
				idle *= 2
			}
			continue
		}
		idle = 200 * time.Millisecond
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			q.exec(ctx, j)
		}()
	}
	wg.Wait()
}

func (q *Queue) known() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	t := make([]string, 0, len(q.types))
	for k := range q.types {
		t = append(t, k)
	}
	return t
}

func (q *Queue) claim(ctx context.Context) (*Job, error) {
	j, err := db.One[Job](ctx, q.db.Pool, `
UPDATE jobs SET status='running', locked_at=now(), locked_by=$1, attempts=attempts+1
WHERE id = (
  SELECT j.id FROM jobs j
  WHERE j.status='pending' AND j.run_after<=now() AND j.type = ANY($3)
    AND (SELECT count(*) FROM jobs r WHERE r.status='running' AND r.org_id=j.org_id) < $2
  ORDER BY j.priority DESC, j.run_after
  LIMIT 1 FOR UPDATE SKIP LOCKED)
RETURNING id, org_id, type, ref_id, payload, attempts, max_attempts`, q.id, q.orgCap, q.known())
	if errors.Is(err, db.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (q *Queue) exec(parent context.Context, j *Job) {
	q.mu.RLock()
	tc := q.types[j.Type]
	q.mu.RUnlock()
	ctx, cancel := context.WithTimeout(parent, tc.timeout)
	defer cancel()
	t0 := time.Now()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return tc.h(ctx, j)
	}()
	bg := context.Background()
	var de DeferError
	var pe PermanentError
	switch {
	case err == nil:
		q.db.Pool.Exec(bg, `UPDATE jobs SET status='done', finished_at=now(), error='' WHERE id=$1 AND status='running'`, j.ID)
	case errors.As(err, &de):
		q.db.Pool.Exec(bg, `UPDATE jobs SET status='pending', attempts=GREATEST(attempts-1,0), run_after=now()+$2::interval, locked_at=NULL WHERE id=$1 AND status='running'`, j.ID, fmt.Sprintf("%d milliseconds", de.After.Milliseconds()))
	case errors.Is(err, ErrCancelled):
		q.db.Pool.Exec(bg, `UPDATE jobs SET status='cancelled', finished_at=now(), error='Stopped by user' WHERE id=$1`, j.ID)
	case errors.As(err, &pe) || j.Attempts >= j.MaxAttempts:
		slog.Warn("job failed", "id", j.ID, "type", j.Type, "org", j.OrgID, "err", err)
		q.db.Pool.Exec(bg, `UPDATE jobs SET status='failed', finished_at=now(), error=$2 WHERE id=$1 AND status='running'`, j.ID, trunc(err.Error(), 2000))
	default:
		back := time.Duration(30<<min(j.Attempts, 6)) * time.Second
		q.db.Pool.Exec(bg, `UPDATE jobs SET status='pending', run_after=now()+$2::interval, error=$3, locked_at=NULL WHERE id=$1 AND status='running'`, j.ID, fmt.Sprintf("%d seconds", int(back.Seconds())), trunc(err.Error(), 2000))
	}
	slog.Debug("job", "id", j.ID, "type", j.Type, "ms", time.Since(t0).Milliseconds(), "err", err)
}

// reaper puts back jobs whose worker died (locked much longer than their timeout).
func (q *Queue) reaper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			q.mu.RLock()
			for typ, c := range q.types {
				q.db.Pool.Exec(ctx, `UPDATE jobs SET status='pending', locked_at=NULL, error='worker stopped – retrying' WHERE status='running' AND type=$1 AND locked_at < now()-$2::interval`,
					typ, fmt.Sprintf("%d seconds", int((c.timeout+5*time.Minute).Seconds())))
			}
			q.mu.RUnlock()
			q.db.Pool.Exec(ctx, `DELETE FROM jobs WHERE status IN ('done','cancelled') AND finished_at < now()-interval '7 days'`)
		}
	}
}

func (q *Queue) scheduler(ctx context.Context) {
	for _, p := range q.periodic {
		q.db.Pool.Exec(ctx, `INSERT INTO schedules (name) VALUES ($1) ON CONFLICT DO NOTHING`, p.name)
	}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, p := range q.periodic {
			n, _ := db.Exec(ctx, q.db.Pool, `UPDATE schedules SET next_at=now()+$2::interval WHERE name=$1 AND next_at<=now()`, p.name, fmt.Sprintf("%d seconds", int(p.every.Seconds())))
			if n == 0 {
				continue
			}
			go func(p periodic) {
				c, cancel := context.WithTimeout(ctx, max(p.every*3, time.Minute))
				defer cancel()
				err := func() (err error) {
					defer func() {
						if r := recover(); r != nil {
							err = fmt.Errorf("panic: %v", r)
						}
					}()
					return p.fn(c)
				}()
				if err != nil {
					slog.Error("periodic", "name", p.name, "err", err)
					q.db.Pool.Exec(context.Background(), `UPDATE schedules SET last_error=$2 WHERE name=$1`, p.name, trunc(err.Error(), 1000))
				} else {
					q.db.Pool.Exec(context.Background(), `UPDATE schedules SET last_ok=now(), last_error='' WHERE name=$1`, p.name)
				}
			}(p)
		}
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
