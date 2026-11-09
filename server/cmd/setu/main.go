// Command setu runs the Setu CRM: `setu serve` (HTTP), `setu worker` (background jobs),
// `setu all` (both, for small servers) and `setu migrate`.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/config"
	"github.com/narendersinghh/setu/server/internal/core"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod"
	"github.com/narendersinghh/setu/server/internal/webui"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cmd := "all"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if err := run(cmd); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cmd string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cmd == "migrate" || cmd == "all" || cmd == "serve" || cmd == "import-v1" {
		if err := db.Migrate(ctx, cfg.MigrateURL, cfg.DatabaseURL); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if cmd == "migrate" {
			return nil
		}
	}
	for _, d := range []string{"files", "reports", "tmp"} {
		os.MkdirAll(filepath.Join(cfg.StorageDir, d), 0o750)
	}
	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer d.Close()
	a := app.New(cfg, d)
	core.Register(a)
	mod.Register(a)
	a.Mux.Handle("/*", webui.Handler())

	switch cmd {
	case "import-v1": // setu import-v1 <export.jsonl> ["Organisation name"]
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: setu import-v1 <export.jsonl> [organisation name]")
		}
		f, err := os.Open(os.Args[2])
		if err != nil {
			return err
		}
		defer f.Close()
		name := ""
		if len(os.Args) > 3 {
			name = os.Args[3]
		}
		id, err := core.ImportV1(ctx, a, f, name)
		if err == nil {
			fmt.Printf("Imported as organisation %d. Users sign in at %s with their v1 email and password.\n", id, cfg.AppURL())
		}
		return err
	case "platform-admin": // setu platform-admin <email> – grant access to the Organisations console
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: setu platform-admin <email>")
		}
		n, err := db.Exec(ctx, d.Pool, `UPDATE users SET platform_admin=true WHERE email=$1`, os.Args[2])
		if err == nil && n == 0 {
			err = fmt.Errorf("no user with that email")
		}
		return err
	case "worker":
		a.Jobs.Run(ctx, cfg.Workers)
		return nil
	case "serve", "all":
		if cmd == "all" {
			go a.Jobs.Run(ctx, cfg.Workers)
		}
		srv := &http.Server{Addr: cfg.Addr, Handler: a.Mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
		go func() {
			<-ctx.Done()
			c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			srv.Shutdown(c)
		}()
		slog.Info("listening", "addr", cfg.Addr, "url", cfg.AppURL())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown command %q (serve | worker | all | migrate | import-v1 | platform-admin)", cmd)
}
