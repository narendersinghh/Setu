// Package settings: per-organisation key/value settings. Secret keys are encrypted at rest.
package settings

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/secure"
)

var secretKey = regexp.MustCompile(`(_key|_pass|_password|_token|_secret)$|^ai_key_`)

// IsSecret reports keys that are encrypted at rest and never sent to the browser.
func IsSecret(k string) bool { return secretKey.MatchString(k) }

type Store struct {
	org int64
	box *secure.Box
	m   map[string]string
}

// Load reads every setting of the organisation of the transaction (RLS-scoped).
func Load(ctx context.Context, q db.Q, box *secure.Box, org int64) (*Store, error) {
	s := &Store{org: org, box: box, m: map[string]string{}}
	rows, err := q.Query(ctx, `SELECT k, v FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if IsSecret(k) {
			v, _ = box.Decrypt(org, v)
		}
		s.m[k] = v
	}
	return s, rows.Err()
}

// Get returns the value or the default when missing/empty.
func (s *Store) Get(k, def string) string {
	if v, ok := s.m[k]; ok && v != "" {
		return v
	}
	return def
}

func (s *Store) Int(k string, def int) int {
	if n, err := strconv.Atoi(s.Get(k, "")); err == nil {
		return n
	}
	return def
}

func (s *Store) Float(k string, def float64) float64 {
	if n, err := strconv.ParseFloat(s.Get(k, ""), 64); err == nil {
		return n
	}
	return def
}

func (s *Store) Bool(k string, def bool) bool {
	switch s.Get(k, "") {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	}
	return def
}

// JSON decodes a JSON setting into out.
func (s *Store) JSON(k string, out any) bool { return json.Unmarshal([]byte(s.Get(k, "")), out) == nil }

// Set saves one value (encrypting secrets) inside the caller's tenant transaction.
func (s *Store) Set(ctx context.Context, q db.Q, k string, v any) error {
	var str string
	switch x := v.(type) {
	case string:
		str = x
	case int:
		str = strconv.Itoa(x)
	case int64:
		str = strconv.FormatInt(x, 10)
	case bool:
		str = map[bool]string{true: "1", false: "0"}[x]
	default:
		b, _ := json.Marshal(x)
		str = string(b)
	}
	stored := str
	if IsSecret(k) {
		stored = s.box.Encrypt(s.org, str)
	}
	if _, err := q.Exec(ctx, `INSERT INTO settings (k, v) VALUES ($1, $2) ON CONFLICT (org_id, k) DO UPDATE SET v = EXCLUDED.v`, k, stored); err != nil {
		return err
	}
	s.m[k] = str
	return nil
}

// Many saves several values.
func (s *Store) Many(ctx context.Context, q db.Q, kv map[string]any) error {
	for k, v := range kv {
		if err := s.Set(ctx, q, k, v); err != nil {
			return err
		}
	}
	return nil
}

// Public returns all settings for the browser: secrets are replaced by "set"/"" flags.
func (s *Store) Public() map[string]any {
	out := make(map[string]any, len(s.m))
	for k, v := range s.m {
		if IsSecret(k) {
			out[k] = map[bool]string{true: "••••••••", false: ""}[v != ""]
			continue
		}
		out[k] = v
	}
	return out
}

// Masked is the placeholder the browser sends back for an unchanged secret.
const Masked = "••••••••"
