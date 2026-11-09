// Package config reads all settings from environment variables (12-factor).
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr         string // listen address, e.g. :8080
	AppHost      string // e.g. app.setu.in – one address for every workspace
	Scheme       string // https in production
	DatabaseURL  string // app role (RLS enforced)
	MigrateURL   string // owner role used for migrations
	MasterKey    []byte // 32 bytes, hex in SETU_MASTER_KEY
	StorageDir   string // uploaded files, PDFs
	GotenbergURL string // http://gotenberg:3000
	Workers      int    // job worker goroutines per process
	OrgJobCap    int    // max running jobs per organisation (fairness)
	Dev          bool
	SignupOpen   bool
	AdminEmail   string // bootstrap platform admin (SETU_ADMIN_EMAIL / SETU_ADMIN_PASSWORD)
	AdminPass    string
	UpdateRepo   string // GitHub owner/name for in-app updates
	UpdateChan   string // stable | edge
	UpdateToken  string // GitHub token for private repositories
}

func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	if n, err := strconv.Atoi(env(k, "")); err == nil {
		return n
	}
	return d
}

func Load() (*Config, error) {
	c := &Config{
		Addr:         env("SETU_ADDR", ":8080"),
		AppHost:      strings.ToLower(env("SETU_APP_HOST", "localhost")),
		Scheme:       env("SETU_SCHEME", "https"),
		DatabaseURL:  env("DATABASE_URL", ""),
		StorageDir:   env("SETU_STORAGE", "./storage"),
		GotenbergURL: env("GOTENBERG_URL", "http://localhost:3000"),
		Workers:      envInt("SETU_WORKERS", 8),
		OrgJobCap:    envInt("SETU_ORG_JOB_CAP", 3),
		Dev:          env("SETU_DEV", "") == "1",
		SignupOpen:   env("SETU_SIGNUP", "1") == "1",
		AdminEmail:   strings.ToLower(env("SETU_ADMIN_EMAIL", "")),
		AdminPass:    env("SETU_ADMIN_PASSWORD", ""),
		UpdateRepo:   env("SETU_UPDATE_REPO", "narendersinghh/setu"),
		UpdateChan:   env("SETU_UPDATE_CHANNEL", "stable"),
		UpdateToken:  env("SETU_UPDATE_TOKEN", ""),
	}
	c.MigrateURL = env("MIGRATE_DATABASE_URL", c.DatabaseURL)
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	k, err := hex.DecodeString(env("SETU_MASTER_KEY", ""))
	if err != nil || len(k) != 32 {
		return nil, fmt.Errorf("SETU_MASTER_KEY must be 64 hex characters (openssl rand -hex 32)")
	}
	c.MasterKey = k
	return c, nil
}

// AppURL is the public address of the app (all workspaces share it).
func (c *Config) AppURL() string {
	if c.Dev && strings.HasPrefix(c.Addr, ":") && c.Addr != ":80" && c.Addr != ":443" && !strings.Contains(c.AppHost, ":") {
		return c.Scheme + "://" + c.AppHost + c.Addr
	}
	return c.Scheme + "://" + c.AppHost
}
