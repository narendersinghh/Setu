package reports

// Templates: AI report prompts and outreach message templates (v1 templates.php + Seed.php).

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/settings"
)

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// PromptTypes: every template type with label and help (v1 Seed::types), in display order.
var PromptTypes = [][3]string{
	{"report_website", "Report · has website", "AI prompt that writes the PDF audit when the business has a website."},
	{"report_noweb", "Report · no website", "AI prompt for businesses without a website (Maps audit + website proposal)."},
	{"whatsapp", "WhatsApp", "First WhatsApp message, sent with the PDF attached. Keep it simple – no fees."},
	{"whatsapp_noweb", "WhatsApp · no website", "First WhatsApp message for businesses without a website."},
	{"email", "Email", "First email, sent with the PDF attached."},
	{"email_noweb", "Email · no website", "First email for businesses without a website (proposal PDF attached)."},
}

func typeOK(t string) bool {
	for _, x := range PromptTypes {
		if x[0] == t {
			return true
		}
	}
	return false
}

var msgVars = []string{"business_name", "contact_name", "first_name", "category", "city", "address", "phone", "email", "website", "rating", "reviews", "keyword", "report_link", "booking_link", "agency_name", "agency_phone", "agency_email", "agency_website", "sender_name", "currency", "currency_symbol", "yearly_fee", "website_fee"}
var aiVars = []string{"web_score", "map_score", "web_categories", "position", "website_audit", "map_audit", "competitors", "services"}

func promptTypes(c *app.Ctx) (any, error) {
	types := []map[string]string{}
	for _, t := range PromptTypes {
		types = append(types, map[string]string{"key": t[0], "label": t[1], "help": t[2]})
	}
	return map[string]any{"types": types, "vars": msgVars, "ai_vars": aiVars}, nil
}

func listPrompts(c *app.Ctx) (any, error) {
	t := c.Query("type")
	if t != "" && !typeOK(t) {
		return nil, app.Bad("Unknown template type.")
	}
	q := `SELECT ` + promptCols + ` FROM prompts`
	args := []any{}
	if t != "" {
		q += ` WHERE type=$1`
		args = append(args, t)
	}
	return db.All[promptRow](c, c.Tx(), q+` ORDER BY type, is_active DESC, updated_at DESC`, args...)
}

func getPrompt(c *app.Ctx) (any, error) {
	p, err := db.One[promptRow](c, c.Tx(), `SELECT `+promptCols+` FROM prompts WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	arts, err := db.All[assetRow](c, c.Tx(), `SELECT id,name,file,placement,prompt_id,created_at FROM assets WHERE prompt_id=$1 ORDER BY placement`, p.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"prompt": p, "assets": arts}, nil
}

// setActive makes one prompt the one in use for its type.
func setActive(c context.Context, tx pgx.Tx, id int64, typ string) error {
	_, err := tx.Exec(c, `UPDATE prompts SET is_active=(id=$1) WHERE type=$2`, id, typ)
	return err
}

func savePrompt(c *app.Ctx) (any, error) {
	var in struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Subject string `json:"subject"`
		Content string `json:"content"`
		Use     bool   `json:"use"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	id := c.ParamID("id")
	tx := c.Tx()
	if id > 0 {
		cur, err := db.One[promptRow](c, tx, `SELECT `+promptCols+` FROM prompts WHERE id=$1`, id)
		if err != nil {
			return nil, app.NotFound()
		}
		in.Type = cur.Type
	}
	if !typeOK(in.Type) {
		return nil, app.Bad("Unknown template type.")
	}
	name := trunc(strings.TrimSpace(in.Name), 180)
	if name == "" {
		name = "Untitled"
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, app.Bad("Content cannot be empty.")
	}
	row := map[string]any{"type": in.Type, "name": name, "subject": trunc(strings.TrimSpace(in.Subject), 250), "content": content}
	if id > 0 {
		if _, err := tx.Exec(c, `UPDATE prompts SET name=$2, subject=$3, content=$4, updated_at=now() WHERE id=$1`, id, row["name"], row["subject"], row["content"]); err != nil {
			return nil, err
		}
	} else {
		var err error
		if id, err = db.Insert(c, tx, "prompts", row); err != nil {
			return nil, err
		}
	}
	n, _ := db.Val[int64](c, tx, `SELECT count(*) FROM prompts WHERE type=$1 AND is_active`, in.Type)
	if in.Use || n == 0 {
		if err := setActive(c, tx, id, in.Type); err != nil {
			return nil, err
		}
	}
	p, err := db.One[promptRow](c, tx, `SELECT `+promptCols+` FROM prompts WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Saved.", "prompt": p}, nil
}

func usePrompt(c *app.Ctx) (any, error) {
	p, err := db.One[promptRow](c, c.Tx(), `SELECT `+promptCols+` FROM prompts WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	if err := setActive(c, c.Tx(), p.ID, p.Type); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Now in use."}, nil
}

func dupPrompt(c *app.Ctx) (any, error) {
	p, err := db.One[promptRow](c, c.Tx(), `SELECT `+promptCols+` FROM prompts WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	id, err := db.Insert(c, c.Tx(), "prompts", map[string]any{"type": p.Type, "name": trunc(p.Name+" (copy)", 180), "subject": p.Subject, "content": p.Content})
	if err != nil {
		return nil, err
	}
	// The copy keeps the prompt's own illustrations
	if _, err := c.Tx().Exec(c, `INSERT INTO assets (name,file,placement,prompt_id) SELECT name,file,placement,$2 FROM assets WHERE prompt_id=$1`, p.ID, id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id}, nil
}

func deletePrompt(c *app.Ctx) (any, error) {
	p, err := db.One[promptRow](c, c.Tx(), `SELECT `+promptCols+` FROM prompts WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	if p.IsActive {
		return nil, app.Bad("You cannot delete the prompt in use. Set another one in use first.")
	}
	if _, err := c.Tx().Exec(c, `DELETE FROM prompts WHERE id=$1`, p.ID); err != nil {
		return nil, err
	}
	if _, err := c.Tx().Exec(c, `UPDATE assets SET prompt_id=NULL WHERE prompt_id=$1`, p.ID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Deleted."}, nil
}

// resetPrompt adds a fresh copy of the default of a type ("Restore default as new").
func resetPrompt(c *app.Ctx) (any, error) {
	var in struct {
		Type string `json:"type"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	tz := c.TZ()
	for _, d := range defaultPrompts {
		if d[0] != in.Type {
			continue
		}
		id, err := db.Insert(c, c.Tx(), "prompts", map[string]any{"type": d[0], "name": d[1] + " (" + nowIn(tz).Format("02 Jan") + ")", "subject": d[2], "content": d[3], "is_default": true})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "id": id}, nil
	}
	return nil, app.Bad("Unknown template type.")
}

// ---------------------------------------------------------------- defaults of a new workspace

// settingDefaults: report & brand settings of a new workspace (v1 Seed::settings, this module's keys).
var settingDefaults = [][2]string{
	{"report_delivery", "both"}, {"report_cta", "Book a free strategy call"},
	{"brand_primary", "#0B0B0B"}, {"brand_accent", "#B8860B"}, {"brand_bg", "#FFFFFF"}, {"brand_text", "#1A1A1A"},
	{"font_heading", "Inter"}, {"font_body", "Inter"},
}

// seed fills a new workspace with the default prompts of every type (report prompts and the email /
// WhatsApp outreach messages used by prospecting) and the report settings.
func seed(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) error {
	have := s.Public()
	for _, kv := range settingDefaults {
		if _, ok := have[kv[0]]; !ok {
			if err := s.Set(ctx, tx, kv[0], kv[1]); err != nil {
				return err
			}
		}
	}
	types := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT DISTINCT type FROM prompts`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t string
		rows.Scan(&t)
		types[t] = true
	}
	rows.Close()
	for _, d := range defaultPrompts {
		if types[d[0]] {
			continue
		}
		if _, err := db.Insert(ctx, tx, "prompts", map[string]any{"type": d[0], "name": d[1], "subject": d[2], "content": d[3], "is_active": true, "is_default": true}); err != nil {
			return err
		}
	}
	return UpgradeReports(ctx, tx)
}

// UpgradeReports adds new default report prompts (by name) to a workspace that already has prompts and
// puts them in use (v1 Seed::upgradeReports) – e.g. after importing a v1 install.
func UpgradeReports(ctx context.Context, tx pgx.Tx) error {
	for _, d := range defaultPrompts {
		if !strings.HasPrefix(d[0], "report_") {
			continue
		}
		if n, _ := db.Val[int64](ctx, tx, `SELECT count(*) FROM prompts WHERE type=$1 AND name=$2`, d[0], d[1]); n > 0 {
			continue
		}
		id, err := db.Insert(ctx, tx, "prompts", map[string]any{"type": d[0], "name": d[1], "subject": d[2], "content": d[3], "is_default": true})
		if err != nil {
			return err
		}
		if err := setActive(ctx, tx, id, d[0]); err != nil {
			return err
		}
	}
	return nil
}
