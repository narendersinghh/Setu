package crm

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

// CSVSafe guards against formula injection: cells starting with = + - @ tab or CR would run as
// formulas in Excel / Sheets, so they get a leading apostrophe (numbers such as -5 stay as they are).
func CSVSafe(v string) string {
	if v == "" || !strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return v
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
		return v
	}
	return "'" + v
}

var exportHead = []string{"Name", "Contact person", "Category", "Address", "City", "Phone", "Alt phone", "Email", "Website", "Source", "Source detail", "Status", "Service", "Deal value", "Tags", "Follow-up", "Rating", "Reviews", "Maps URL", "Website score", "Maps score", "Outreach stage", "Email status", "WhatsApp status", "Replied at", "Created"}

// ExportCSV streams leads matching where (leads aliased as l) as a CSV download (v1 Outreach::csv).
// Used for the Leads export; prospecting can call it for Scraped Data.
func ExportCSV(c *app.Ctx, where string, args []any) error {
	tx := c.Tx()
	fs, err := Fields(c, tx)
	if err != nil {
		return err
	}
	loc := c.TZ()
	rows, err := tx.Query(c, `SELECT l.name,l.contact_name,l.category,l.address,l.city,l.phone,l.alt_phone,l.email,l.website,l.source,l.source_detail,l.status,l.service,
 COALESCE(l.deal_value::text,''),l.tags,l.follow_up_at,COALESCE(l.rating::text,''),COALESCE(l.reviews::text,''),l.maps_url,COALESCE(l.web_score::text,''),COALESCE(l.map_score::text,''),
 l.stage,l.email_status,l.wa_status,l.replied_at,l.created_at,l.custom
FROM leads l WHERE `+where+` ORDER BY l.id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	c.Raw()
	h := c.W.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="setu-`+time.Now().In(loc).Format("20060102-150405")+`.csv"`)
	h.Set("Cache-Control", "no-store")
	bw := bufio.NewWriterSize(c.W, 64<<10)
	bw.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(bw)
	head := append([]string{}, exportHead...)
	for _, f := range fs {
		head = append(head, f.Label)
	}
	w.Write(head)
	ts := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.In(loc).Format("2006-01-02 15:04:05")
	}
	for rows.Next() {
		var s [25]string
		var fu, rep *time.Time
		var created time.Time
		var custom map[string]any
		if err := rows.Scan(&s[0], &s[1], &s[2], &s[3], &s[4], &s[5], &s[6], &s[7], &s[8], &s[9], &s[10], &s[11], &s[12], &s[13], &s[14], &fu, &s[15], &s[16], &s[17], &s[18], &s[19], &s[20], &s[21], &s[22], &rep, &created, &custom); err != nil {
			return err
		}
		stage := s[20]
		if st, ok := shared.Stages[stage]; ok {
			stage = st[0]
		}
		rec := []string{s[0], s[1], s[2], s[3], s[4], s[5], s[6], s[7], s[8], s[9], s[10], shared.StatusLabel(s[11]), s[12], s[13], s[14], ts(fu), s[15], s[16], s[17], s[18], s[19], stage, s[21], s[22], ts(rep), ts(&created)}
		for _, f := range fs {
			v := ""
			switch x := custom[f.Key].(type) {
			case []any:
				v = joinScalars(x)
			case nil:
			default:
				v, _ = scalar(x)
			}
			rec = append(rec, v)
		}
		for i := range rec {
			rec[i] = CSVSafe(rec[i])
		}
		if err := w.Write(rec); err != nil {
			return nil // client went away
		}
	}
	w.Flush()
	bw.Flush()
	return rows.Err()
}

func leadExport(c *app.Ctx) (any, error) {
	q := leadFilter(c, true)
	return nil, ExportCSV(c, q.where(), q.args)
}

// ImportCSV adds leads from a CSV file with a header row (v1 CRM::import). Returns the count.
func ImportCSV(c *app.Ctx, r io.Reader) (int, error) {
	tx := c.Tx()
	fs, err := Fields(c, tx)
	if err != nil {
		return 0, err
	}
	keys := FieldKeys(fs)
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.ReuseRecord = false
	head, err := cr.Read()
	if err != nil || len(head) == 0 {
		return 0, app.Bad("Empty CSV")
	}
	head[0] = strings.TrimPrefix(head[0], "\xEF\xBB\xBF")
	pos := map[string]int{}
	for i, h := range head {
		if _, ok := pos[Key(h)]; !ok {
			pos[Key(h)] = i
		}
	}
	detail := "CSV " + time.Now().In(c.TZ()).Format("02 Jan 2006")
	n, line := 0, 1
	for {
		row, err := cr.Read()
		line++
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return n, app.Bad(fmt.Sprintf("Line %d of the CSV cannot be read: %v", line, err))
		}
		empty := true
		for _, v := range row {
			if v != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		if n >= 50000 {
			return n, errTooMany
		}
		in := map[string]any{}
		for i, h := range head {
			v := ""
			if i < len(row) {
				v = row[i]
			}
			in[h] = v
		}
		d, cf, rest, _ := normalizeOrdered(in, nil, keys)
		for i, h := range head {
			k := strings.ToLower(nonAlnum.ReplaceAllString(h, "_"))
			if i < len(row) && coreSet[k] && d[k] == "" && row[i] != "" {
				d[k] = row[i]
			}
		}
		if len(rest) > 0 {
			sort.SliceStable(rest, func(i, j int) bool { return pos[rest[i].k] < pos[rest[j].k] })
			lines := make([]string, len(rest))
			for i, x := range rest {
				lines[i] = x.k + ": " + x.v
			}
			d["message"] = strings.TrimSpace(d["message"] + "\n" + strings.Join(lines, "\n"))
		}
		if _, err := Create(c, c.A, tx, c.Org.ID, CreateIn{Fields: d, Custom: cf, Source: "import", Detail: detail}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func leadImport(c *app.Ctx) (any, error) {
	c.R.Body = http.MaxBytesReader(c.W, c.R.Body, 64<<20)
	if err := c.R.ParseMultipartForm(16 << 20); err != nil {
		return nil, app.Bad("Choose a CSV file (up to 64 MB).")
	}
	f, _, err := c.R.FormFile("csv")
	if err != nil {
		if f, _, err = c.R.FormFile("file"); err != nil {
			return nil, app.Bad("Choose a CSV file.")
		}
	}
	defer f.Close()
	n, err := ImportCSV(c, f)
	if errors.Is(err, errTooMany) {
		return map[string]any{"ok": true, "n": n, "message": strconv.Itoa(n) + " lead(s) imported. Import up to 50,000 rows at a time – the rest of the file was skipped."}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "n": n, "message": strconv.Itoa(n) + " lead(s) imported."}, nil
}

var errTooMany = errors.New("too many rows")
