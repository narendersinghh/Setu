// Package db wraps a pgx pool. Tenant data is only reachable through Org(), which opens a
// transaction with app.org_id set so Postgres Row-Level Security isolates every query.
package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Q is anything that can run queries: the pool (global tables) or a tenant transaction.
type Q interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type DB struct{ Pool *pgxpool.Pool }

var ErrNoRows = pgx.ErrNoRows

func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := p.Ping(ctx); err != nil {
		return nil, err
	}
	return &DB{Pool: p}, nil
}

func (d *DB) Close() { d.Pool.Close() }

// Org runs fn in a transaction scoped to one organisation. Commits when fn returns nil.
func (d *DB) Org(ctx context.Context, orgID int64, fn func(tx pgx.Tx) error) error {
	if orgID <= 0 {
		return errors.New("db: organisation required")
	}
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if err := SetOrg(ctx, tx, orgID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// SetOrg scopes an open transaction to an organisation.
func SetOrg(ctx context.Context, tx pgx.Tx, orgID int64) error {
	_, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", strconv.FormatInt(orgID, 10))
	return err
}

// Begin opens a tenant transaction the caller must Commit/Rollback (used by the request context).
func (d *DB) Begin(ctx context.Context, orgID int64) (pgx.Tx, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err := SetOrg(ctx, tx, orgID); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// ---- generic helpers (work with the pool or a transaction)

// All scans rows into structs by column name (db:"col" tags, missing columns ignored).
func All[T any](ctx context.Context, q Q, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if out == nil {
		out = []T{}
	}
	return out, err
}

// One scans the first row into a struct; returns ErrNoRows when nothing matched.
func One[T any](ctx context.Context, q Q, sql string, args ...any) (T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		var z T
		return z, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[T])
}

// Maps returns rows as maps (handy for JSON APIs with dynamic columns).
func Maps(ctx context.Context, q Q, sql string, args ...any) ([]map[string]any, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToMap)
	if out == nil {
		out = []map[string]any{}
	}
	return out, err
}

// Map returns one row as a map, or nil when nothing matched.
func Map(ctx context.Context, q Q, sql string, args ...any) (map[string]any, error) {
	m, err := Maps(ctx, q, sql, args...)
	if err != nil || len(m) == 0 {
		return nil, err
	}
	return m[0], nil
}

// Val returns a single value; the zero value when nothing matched.
func Val[T any](ctx context.Context, q Q, sql string, args ...any) (T, error) {
	var v T
	err := q.QueryRow(ctx, sql, args...).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, nil
	}
	return v, err
}

// Exec runs a statement and returns affected rows.
func Exec(ctx context.Context, q Q, sql string, args ...any) (int64, error) {
	t, err := q.Exec(ctx, sql, args...)
	return t.RowsAffected(), err
}

// Insert builds INSERT … RETURNING id from a column map.
func Insert(ctx context.Context, q Q, table string, cols map[string]any) (int64, error) {
	if len(cols) == 0 {
		return 0, errors.New("db: insert without columns")
	}
	names, ph, args := "", "", make([]any, 0, len(cols))
	i := 0
	for k, v := range cols {
		if i > 0 {
			names += ","
			ph += ","
		}
		i++
		names += pgx.Identifier{k}.Sanitize()
		ph += "$" + strconv.Itoa(i)
		args = append(args, v)
	}
	var id int64
	err := q.QueryRow(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id", pgx.Identifier{table}.Sanitize(), names, ph), args...).Scan(&id)
	return id, err
}

// Update builds UPDATE table SET … WHERE id=$n.
func Update(ctx context.Context, q Q, table string, id int64, cols map[string]any) error {
	if len(cols) == 0 {
		return nil
	}
	set, args := "", make([]any, 0, len(cols)+1)
	i := 0
	for k, v := range cols {
		if i > 0 {
			set += ","
		}
		i++
		set += pgx.Identifier{k}.Sanitize() + "=$" + strconv.Itoa(i)
		args = append(args, v)
	}
	args = append(args, id)
	_, err := q.Exec(ctx, fmt.Sprintf("UPDATE %s SET %s WHERE id=$%d", pgx.Identifier{table}.Sanitize(), set, i+1), args...)
	return err
}

// IsUnique reports a unique-constraint violation.
func IsUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
