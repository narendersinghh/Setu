package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// ImportV1 loads a v1 export (tools/export-v2.php) as one new organisation. IDs are remapped,
// timestamps converted from the install's time zone, secrets re-encrypted with the workspace key.
// Existing v1 passwords keep working (bcrypt is verified and upgraded to argon2id on first login).
func ImportV1(ctx context.Context, a *app.App, r io.Reader, orgName string) (int64, error) {
	cols, err := columnTypes(ctx, a.DB)
	if err != nil {
		return 0, err
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	imp := &importer{a: a, cols: cols, ids: map[string]map[string]int64{}, tz: time.UTC, counts: map[string]int{}}
	var orgID int64
	err = pgx.BeginFunc(ctx, a.DB.Pool, func(tx pgx.Tx) error {
		imp.tx = tx
		var pendingSettings [][2]string
		for sc.Scan() {
			var line struct {
				T string         `json:"t"`
				R map[string]any `json:"r"`
			}
			if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
				return fmt.Errorf("bad line: %w", err)
			}
			switch line.T {
			case "meta":
				imp.tz = app.LoadTZ(str(line.R["timezone"]))
			case "settings":
				pendingSettings = append(pendingSettings, [2]string{str(line.R["k"]), str(line.R["v"])})
			default:
				if orgID == 0 {
					name := orgName
					for _, kv := range pendingSettings {
						if kv[0] == "company_name" && name == "" {
							name = kv[1]
						}
					}
					if name == "" {
						name = "Imported workspace"
					}
					if err := tx.QueryRow(ctx, `INSERT INTO orgs (slug,name) VALUES ($1,$2) RETURNING id`, slugify(name)+"-"+strconv.FormatInt(time.Now().Unix()%100000, 10), name).Scan(&orgID); err != nil {
						return err
					}
					if err := db.SetOrg(ctx, tx, orgID); err != nil {
						return err
					}
					imp.org = orgID
					s, err := settings.Load(ctx, tx, a.Box, orgID)
					if err != nil {
						return err
					}
					if err := a.RunSeeds(ctx, tx, orgID, s); err != nil {
						return err
					}
					for _, kv := range pendingSettings {
						if skipSetting.MatchString(kv[0]) {
							continue
						}
						if err := s.Set(ctx, tx, kv[0], kv[1]); err != nil {
							return err
						}
					}
				}
				if err := imp.row(ctx, line.T, line.R); err != nil {
					return fmt.Errorf("%s: %w", line.T, err)
				}
			}
		}
		if err := sc.Err(); err != nil {
			return err
		}
		if orgID == 0 {
			return fmt.Errorf("export file has no data")
		}
		return nil
	})
	slog.Info("import-v1", "org", orgID, "rows", imp.counts)
	return orgID, err
}

var skipSetting = regexp.MustCompile(`^(cron_|db_version|installed_sha|update_|scheduler$|queue_budget|workers$|app_url|scan_last|job_timeout|cron)`)

type importer struct {
	a      *app.App
	tx     pgx.Tx
	org    int64
	cols   map[string]map[string]string
	ids    map[string]map[string]int64 // table → old id → new id
	tz     *time.Location
	counts map[string]int
}

// references to remap: table.column → parent table
var refs = map[string]string{
	"leads.campaign_id": "campaigns", "leads.assigned_to": "users",
	"reports.lead_id": "leads", "reports.prompt_id": "prompts", "assets.prompt_id": "prompts",
	"events.lead_id": "leads", "messages.lead_id": "leads", "calls.lead_id": "leads",
	"automation_runs.lead_id": "leads", "automation_runs.automation_id": "automations",
	"appointments.lead_id": "leads", "appointments.user_id": "users",
}

func (im *importer) mapID(table, old string) int64 {
	return im.ids[table][old]
}

func (im *importer) row(ctx context.Context, table string, r map[string]any) error {
	if table == "users" {
		return im.user(ctx, r)
	}
	cols := im.cols[table]
	if cols == nil {
		return nil
	}
	names, vals := []string{}, []any{}
	for k, v := range r {
		typ, ok := cols[k]
		if !ok || k == "id" || k == "org_id" {
			continue
		}
		if parent, ok := refs[table+"."+k]; ok {
			s := str(v)
			if s == "" || s == "0" {
				if typ == "bigint" && (k == "assigned_to" || k == "user_id" || k == "prompt_id") {
					v = nil
				} else {
					v = int64(0)
				}
			} else if n := im.mapID(parent, s); n > 0 {
				v = n
			} else if k == "assigned_to" || k == "user_id" || k == "prompt_id" {
				v = nil
			} else {
				return nil // orphan row (parent missing in v1) – skip
			}
			names, vals = append(names, k), append(vals, v)
			continue
		}
		cv, err := im.convert(table, k, typ, v)
		if err != nil {
			return err
		}
		if cv == skip {
			continue
		}
		names, vals = append(names, k), append(vals, cv)
	}
	if len(names) == 0 {
		return nil
	}
	ph := make([]string, len(names))
	for i := range ph {
		ph[i] = "$" + strconv.Itoa(i+1)
		names[i] = pgx.Identifier{names[i]}.Sanitize()
	}
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, table, strings.Join(names, ","), strings.Join(ph, ","))
	switch table {
	case "api_usage", "research_cache":
		_, err := im.tx.Exec(ctx, q+` ON CONFLICT DO NOTHING`, vals...)
		im.counts[table]++
		return err
	}
	if _, ok := cols["id"]; !ok {
		_, err := im.tx.Exec(ctx, q, vals...)
		return err
	}
	if _, err := im.tx.Exec(ctx, "SAVEPOINT r"); err != nil {
		return err
	}
	var id int64
	if err := im.tx.QueryRow(ctx, q+` RETURNING id`, vals...).Scan(&id); err != nil {
		im.tx.Exec(ctx, "ROLLBACK TO SAVEPOINT r")
		if db.IsUnique(err) { // e.g. duplicate token/place – skip the row, keep importing
			slog.Warn("import-v1: duplicate row skipped", "table", table, "id", str(r["id"]))
			return nil
		}
		return err
	}
	if im.ids[table] == nil {
		im.ids[table] = map[string]int64{}
	}
	im.ids[table][str(r["id"])] = id
	im.counts[table]++
	return nil
}

type skipT struct{}

var skip = skipT{}

var jsonDefaults = map[string]string{"custom": "{}", "config": "{}", "conditions": "[]", "steps": "[]", "log": "[]", "payload": "{}"}

func (im *importer) convert(table, col, typ string, v any) (any, error) {
	s, isNull := str(v), v == nil
	switch typ {
	case "boolean":
		return s == "1" || strings.EqualFold(s, "true"), nil
	case "jsonb":
		if !isNull && s != "" && json.Valid([]byte(s)) {
			return s, nil
		}
		if d, ok := jsonDefaults[col]; ok {
			if col == "log" && s != "" {
				b, _ := json.Marshal([]string{s})
				return string(b), nil
			}
			return d, nil
		}
		return nil, nil
	case "timestamp with time zone", "date":
		if isNull || s == "" || strings.HasPrefix(s, "0000") {
			if col == "created_at" || col == "updated_at" {
				return skip, nil // keep DEFAULT now()
			}
			if col == "start_at" || col == "end_at" || col == "day" {
				return time.Now(), nil
			}
			return nil, nil
		}
		for _, f := range []string{"2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.ParseInLocation(f, s, im.tz); err == nil {
				return t, nil
			}
		}
		return nil, nil
	case "smallint", "integer", "bigint":
		if isNull || s == "" {
			return nil, nil
		}
		n, _ := strconv.ParseInt(s, 10, 64)
		return n, nil
	case "numeric":
		if isNull || s == "" {
			return nil, nil
		}
		return s, nil
	default: // text, citext
		if isNull {
			return "", nil
		}
		return s, nil
	}
}

func (im *importer) user(ctx context.Context, r map[string]any) error {
	email := strings.ToLower(str(r["email"]))
	var id int64
	err := im.tx.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&id)
	if err != nil {
		if err := im.tx.QueryRow(ctx, `INSERT INTO users (email,name,password_hash,phone) VALUES ($1,$2,$3,$4) RETURNING id`,
			email, str(r["name"]), str(r["password"]), str(r["phone"])).Scan(&id); err != nil {
			return err
		}
	}
	role := str(r["role"])
	if role != "super" && role != "admin" {
		role = "sales"
	}
	active := str(r["active"]) != "0"
	if _, err := im.tx.Exec(ctx, `INSERT INTO memberships (org_id,user_id,role,active) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, im.org, id, role, active); err != nil {
		return err
	}
	if im.ids["users"] == nil {
		im.ids["users"] = map[string]int64{}
	}
	im.ids["users"][str(r["id"])] = id
	im.counts["users"]++
	return nil
}

func columnTypes(ctx context.Context, d *db.DB) (map[string]map[string]string, error) {
	rows, err := d.Pool.Query(ctx, `SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema='public'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var t, c, typ string
		if err := rows.Scan(&t, &c, &typ); err != nil {
			return nil, err
		}
		if out[t] == nil {
			out[t] = map[string]string{}
		}
		if typ == "USER-DEFINED" {
			typ = "text"
		}
		out[t][c] = typ
	}
	return out, rows.Err()
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "1"
		}
		return "0"
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
