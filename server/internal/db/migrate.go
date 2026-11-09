package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies pending SQL migrations (as the owner role) and grants the app role access.
// Each file runs in its own transaction; applied versions are recorded in schema_migrations.
func Migrate(ctx context.Context, ownerURL, appURL string) error {
	d, err := Open(ctx, ownerURL)
	if err != nil {
		return err
	}
	defer d.Close()
	if _, err := d.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	// Only one migrator at a time across all app servers
	if _, err := d.Pool.Exec(ctx, `SELECT pg_advisory_lock(727274)`); err != nil {
		return err
	}
	defer d.Pool.Exec(context.Background(), `SELECT pg_advisory_unlock(727274)`)
	files, _ := fs.Glob(migrations, "migrations/*.sql")
	sort.Strings(files)
	for _, f := range files {
		v := strings.TrimSuffix(strings.TrimPrefix(f, "migrations/"), ".sql")
		var done bool
		if err := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, v).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, _ := migrations.ReadFile(f)
		err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, v)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", v, err)
		}
		slog.Info("migrated", "version", v)
	}
	// Least privilege: the app role may read/write data but never change the schema or bypass RLS
	if u, err := url.Parse(appURL); err == nil && u.User != nil {
		if owner, _ := url.Parse(ownerURL); owner == nil || owner.User == nil || owner.User.Username() != u.User.Username() {
			r := pgx.Identifier{u.User.Username()}.Sanitize()
			for _, s := range []string{
				"GRANT USAGE ON SCHEMA public TO " + r,
				"GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO " + r,
				"GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO " + r,
				"REVOKE ALL ON schema_migrations FROM " + r,
			} {
				if _, err := d.Pool.Exec(ctx, s); err != nil {
					return fmt.Errorf("grant: %w", err)
				}
			}
		}
	}
	return nil
}
