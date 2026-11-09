package crm

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// FieldTypes: custom field types and their labels (v1 settings → Custom fields).
var FieldTypes = [][2]string{{"text", "Text"}, {"textarea", "Long text"}, {"number", "Number"}, {"date", "Date"}, {"select", "Dropdown"}, {"checkbox", "Yes / No"}, {"email", "Email"}, {"phone", "Phone"}, {"url", "Link"}}

func validType(t string) bool {
	for _, x := range FieldTypes {
		if x[0] == t {
			return true
		}
	}
	return false
}

// defaultFields: custom fields of a new workspace (v1 Seed::fields).
var defaultFields = [][4]string{
	{"business_type", "Business type", "select", "Retail\nClinic / Healthcare\nRestaurant / Café\nReal estate\nEducation\nProfessional services\nManufacturing\nOther"},
	{"budget", "Monthly budget", "number", ""},
	{"requirement", "Requirement", "textarea", ""},
	{"preferred_time", "Preferred contact time", "select", "Morning\nAfternoon\nEvening"},
}

func registerFields(a *app.App) {
	a.Get("/fields", fieldList, app.Perm("leads"))
	a.Post("/fields", fieldAdd, app.Perm("settings"))
	a.Put("/fields", fieldSave, app.Perm("settings"))
	a.Delete("/fields/{id}", fieldDelete, app.Perm("settings"))
	a.Seed(func(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) error {
		n, err := db.Val[int](ctx, tx, `SELECT count(*) FROM fields`)
		if err != nil || n > 0 {
			return err
		}
		for i, f := range defaultFields {
			if _, err := tx.Exec(ctx, `INSERT INTO fields (fkey,label,type,options,sort) VALUES ($1,$2,$3,$4,$5)`, f[0], f[1], f[2], f[3], i); err != nil {
				return err
			}
		}
		return nil
	})
}

func fieldList(c *app.Ctx) (any, error) {
	fs, err := Fields(c, c.Tx())
	return map[string]any{"fields": fs, "types": FieldTypes}, err
}

var fkeyRe = regexp.MustCompile(`[^a-z0-9]+`)

func fieldAdd(c *app.Ctx) (any, error) {
	var in struct {
		Label    string `json:"label"`
		Key      string `json:"fkey"`
		Type     string `json:"type"`
		Options  string `json:"options"`
		Required bool   `json:"required"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		return nil, app.Bad("Enter a field name.")
	}
	k := strings.ToLower(strings.TrimSpace(in.Key))
	if k == "" {
		k = strings.ToLower(in.Label)
	}
	k = strings.Trim(fkeyRe.ReplaceAllString(k, "_"), "_")
	if k == "" {
		return nil, app.Bad("Enter a key with letters or numbers.")
	}
	k = cut(k, 60)
	if !validType(in.Type) {
		in.Type = "text"
	}
	tx := c.Tx()
	exists, err := db.Val[bool](c, tx, `SELECT EXISTS(SELECT 1 FROM fields WHERE fkey=$1)`, k)
	if err != nil {
		return nil, err
	}
	if coreSet[k] || exists || k == "_extra" {
		return nil, app.Bad(`A field with key "` + k + `" already exists.`)
	}
	id, err := db.Insert(c, tx, "fields", map[string]any{"fkey": k, "label": cut(in.Label, 110), "type": in.Type, "options": strings.TrimSpace(in.Options), "required": in.Required,
		"sort": dbSortNext(c)})
	if db.IsUnique(err) {
		return nil, app.Bad(`A field with key "` + k + `" already exists.`)
	}
	return map[string]any{"ok": true, "id": id, "message": "Field added."}, err
}

func dbSortNext(c *app.Ctx) int {
	n, _ := db.Val[int](c, c.Tx(), `SELECT COALESCE(max(sort),0)+1 FROM fields`)
	return n
}

func fieldSave(c *app.Ctx) (any, error) {
	var in struct {
		Fields []Field `json:"fields"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	tx := c.Tx()
	for _, f := range in.Fields {
		f.Label = strings.TrimSpace(f.Label)
		if f.Label == "" {
			return nil, app.Bad("Every field needs a label.")
		}
		if !validType(f.Type) {
			f.Type = "text"
		}
		if _, err := tx.Exec(c, `UPDATE fields SET label=$2,type=$3,options=$4,required=$5,sort=$6 WHERE id=$1`, f.ID, cut(f.Label, 110), f.Type, strings.TrimSpace(f.Options), f.Required, f.Sort); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "message": "Fields saved."}, nil
}

func fieldDelete(c *app.Ctx) (any, error) {
	n, err := db.Exec(c, c.Tx(), `DELETE FROM fields WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, app.NotFound()
	}
	return map[string]any{"ok": true, "message": "Field deleted (existing values stay in the lead data)."}, nil
}
