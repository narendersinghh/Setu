// Package usage: API usage & costs per workspace – service catalog with free limits, editable prices,
// month-end projection, currency conversion and an AI price check. Ports v1 Usage.php and usage.php.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/ai"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/settings"
)

func Register(a *app.App) {
	a.Get("/usage", func(c *app.Ctx) (any, error) { return page(c) }, app.Perm("settings"))
	a.Put("/usage/prices", savePrices, app.Perm("settings"))
	a.Post("/usage/ai-prices", startAIPrices, app.Perm("settings"))
	a.Get("/usage/ai-prices", func(c *app.Ctx) (any, error) { return priceUpdate(c.S()), nil }, app.Perm("settings"))
	a.Jobs.Handle("prices", 6*time.Minute, func(ctx context.Context, j *jobs.Job) error { return runAIPrices(ctx, a, j.OrgID) })
}

// ---------------------------------------------------------------- month summary

type monthRow struct {
	Service string  `db:"service"`
	Calls   int64   `db:"calls"`
	In      int64   `db:"uin"`
	Out     int64   `db:"uout"`
	Cost    float64 `db:"cost"`
	Errors  int64   `db:"errors"`
}

type svcRow struct {
	Service
	Group    string    `json:"group"`
	Status   [2]string `json:"status"`
	Used     float64   `json:"used"`  // units this month (or today for daily limits)
	Units    float64   `json:"units"` // units this month
	Proj     float64   `json:"proj"`  // projected units by month end
	Calls    int64     `json:"calls"`
	In       int64     `json:"in"`
	Out      int64     `json:"out"`
	Errors   int64     `json:"errors"`
	CostRaw  float64   `json:"cost_raw"` // cost recorded with each call
	Bill     float64   `json:"bill"`     // estimated cost after free tiers
	BillProj float64   `json:"bill_proj"`
	Pct      *float64  `json:"pct"` // share of the free limit used
	Has      bool      `json:"has"` // used this month
}

func page(c *app.Ctx) (any, error) {
	rates := fxRates(c) // outside the transaction: may call the rates API once a day
	s := c.S()
	tx := c.Tx()
	var today time.Time
	if err := tx.QueryRow(c, `SELECT current_date`).Scan(&today); err != nil {
		return nil, err
	}
	d := today.Day()
	dim := time.Date(today.Year(), today.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	rows, err := db.All[monthRow](c, tx, `SELECT service, SUM(calls)::bigint calls, SUM(units_in)::bigint uin, SUM(units_out)::bigint uout, SUM(cost)::float8 cost, SUM(errors)::bigint errors
FROM api_usage WHERE day >= date_trunc('month', current_date)::date GROUP BY service`)
	if err != nil {
		return nil, err
	}
	m := map[string]monthRow{}
	for _, r := range rows {
		m[r.Service] = r
	}
	todayCalls := map[string]int64{}
	perDay := map[string][]int64{}
	trs, err := db.All[struct {
		Service string    `db:"service"`
		Day     time.Time `db:"day"`
		N       int64     `db:"n"`
	}](c, tx, `SELECT service, day, SUM(calls)::bigint n FROM api_usage WHERE day >= date_trunc('month', current_date)::date GROUP BY service, day`)
	if err != nil {
		return nil, err
	}
	for _, r := range trs {
		perDay[r.Service] = append(perDay[r.Service], r.N)
		if r.Day.Equal(today) {
			todayCalls[r.Service] = r.N
		}
	}
	P := LoadPrices(s)
	var outGroups []map[string]any
	var tc, tp float64
	var calls int64
	warn := []string{}
	for _, g := range groups {
		var list []svcRow
		for _, k := range g.Keys {
			cat := catalogByKey[k]
			pr := P.Svc[k]
			row := svcRow{Service: cat, Group: g.Name, Status: status(s, k)}
			row.Price, row.Free = pr.Price, pr.Free
			if r, ok := m[k]; ok {
				row.Has = true
				row.Calls, row.In, row.Out, row.Errors, row.CostRaw = r.Calls, r.In, r.Out, r.Errors, r.Cost
				switch cat.Unit {
				case "minutes":
					row.Units = math.Round(float64(r.In)/60*10) / 10
				case "tokens":
					row.Units = float64(r.In + r.Out)
				default:
					row.Units = float64(r.Calls)
				}
				row.Used, row.Proj = row.Units, math.Round(row.Units/float64(d)*float64(dim))
				if cat.Per == "day" {
					row.Used = float64(todayCalls[k])
					row.Proj = row.Used
				}
			}
			if row.Free > 0 {
				p := math.Min(999, math.Round(row.Used/row.Free*100))
				row.Pct = &p
			}
			// estimated bill after free tiers
			switch {
			case cat.Unit == "tokens":
				row.Bill = row.CostRaw
				row.BillProj = math.Round(row.CostRaw/float64(d)*float64(dim)*100) / 100
			case cat.Per == "day":
				for _, n := range perDay[k] {
					row.Bill += math.Max(0, float64(n)-row.Free) * row.Price
				}
				row.BillProj = row.Bill / float64(d) * float64(dim)
			default:
				row.Bill = math.Max(0, row.Units-row.Free) * row.Price
				row.BillProj = math.Max(0, row.Proj-row.Free) * row.Price
			}
			tc += row.Bill
			tp += row.BillProj
			calls += row.Calls
			if row.Has && row.Free > 0 && row.Proj > row.Free {
				warn = append(warn, cat.Name)
			}
			list = append(list, row)
		}
		outGroups = append(outGroups, map[string]any{"name": g.Name, "ic": g.Ic, "c": g.C, "rows": list})
	}
	// last 30 days
	dr, err := db.All[struct {
		Day   time.Time `db:"day"`
		Calls int64     `db:"calls"`
		Cost  float64   `db:"cost"`
	}](c, tx, `SELECT day, SUM(calls)::bigint calls, SUM(cost)::float8 cost FROM api_usage WHERE day >= current_date - 29 GROUP BY day`)
	if err != nil {
		return nil, err
	}
	byDay := map[string][2]float64{}
	for _, r := range dr {
		byDay[r.Day.Format("2006-01-02")] = [2]float64{r.Cost, float64(r.Calls)}
	}
	daily := make([]map[string]any, 0, 30)
	for i := 29; i >= 0; i-- {
		dd := today.AddDate(0, 0, -i).Format("2006-01-02")
		v := byDay[dd]
		daily = append(daily, map[string]any{"day": dd, "cost": v[0], "calls": int64(v[1])})
	}
	// models
	provider := s.Get("ai_provider", "anthropic")
	inUse := s.Get("ai_model_"+provider, "")
	models := []map[string]any{}
	for id, x := range P.Models {
		models = append(models, map[string]any{"id": id, "in": x.In, "out": x.Out, "in_use": id == inUse})
	}
	sortModels(models)
	fxi := currency(c, tx, s, rates)
	return map[string]any{
		"month":        today.Format("January 2006"),
		"month_name":   today.Format("January"),
		"cost":         tc,
		"proj":         tp,
		"calls":        calls,
		"services":     len(m),
		"warn":         warn,
		"groups":       outGroups,
		"daily":        daily,
		"models":       models,
		"fx":           fxi,
		"price_update": priceUpdate(s),
		"web_search":   provider == "anthropic",
	}, nil
}

func sortModels(m []map[string]any) {
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && m[j]["id"].(string) < m[j-1]["id"].(string); j-- {
			m[j], m[j-1] = m[j-1], m[j]
		}
	}
}

// status: is the API set up / in use? [label, colour] (v1 Usage::status; settings keys as in v1).
func status(s *settings.Store, k string) [2]string {
	has := func(x string) bool { return s.Get(x, "") != "" }
	aiP := s.Get("ai_provider", "anthropic")
	switch k {
	case "serpapi":
		if !has("serpapi_key") {
			return [2]string{"Not added", "gray"}
		}
		if s.Get("maps_source", "places") == "serpapi" {
			return [2]string{"Active", "green"}
		}
		return [2]string{"Key added", "teal"}
	case "pagespeed":
		if has("google_api_key") && s.Get("pagespeed", "") == "1" {
			return [2]string{"Active", "green"}
		}
		return [2]string{"Off", "gray"}
	case "web_search":
		if aiP == "anthropic" && has("ai_key_anthropic") && s.Get("ai_web_research", "1") == "1" {
			return [2]string{"Active", "green"}
		}
		return [2]string{"Off", "gray"}
	case "whatsapp":
		if s.Get("wa_provider", "") == "cloud" {
			return [2]string{"Active", "green"}
		}
		return [2]string{"Not added", "gray"}
	case "whatsapp_api":
		if s.Get("wa_provider", "") == "generic" {
			return [2]string{"Active", "green"}
		}
		return [2]string{"Not added", "gray"}
	case "email":
		switch s.Get("mail_transport", "") {
		case "api":
			if has("mail_api_key") {
				return [2]string{"Active · API", "green"}
			}
		case "smtp":
			if has("smtp_host") {
				return [2]string{"Active · SMTP", "green"}
			}
		}
		return [2]string{"Not set up", "amber"}
	case "gcal":
		if has("gcal_client_id") && has("gcal_refresh_token") {
			return [2]string{"Connected", "green"}
		}
		return [2]string{"Not connected", "gray"}
	}
	if strings.HasPrefix(k, "google_places") {
		atmo := s.Get("maps_atmosphere", "0") == "1"
		switch {
		case !has("google_api_key"):
			return [2]string{"Not added", "gray"}
		case k == "google_places_ids":
			return [2]string{"Free · automatic", "green"}
		case strings.Contains(k, "_atmo"):
			if atmo {
				return [2]string{"Active", "green"}
			}
			return [2]string{"Off", "gray"}
		case atmo:
			return [2]string{"Off", "gray"}
		case s.Get("maps_source", "places") == "places":
			return [2]string{"Active", "green"}
		}
		return [2]string{"Key added", "teal"}
	}
	if strings.HasPrefix(k, "voice_") {
		if s.Get("voice_provider", "") == k[6:] {
			return [2]string{"Active", "green"}
		}
		return [2]string{"Not added", "gray"}
	}
	switch k {
	case "anthropic", "openai", "openrouter", "gemini", "groq", "deepseek", "custom":
		if !has("ai_key_" + k) {
			return [2]string{"Not added", "gray"}
		}
		if aiP == k {
			return [2]string{"Active", "green"}
		}
		return [2]string{"Key added", "teal"}
	}
	return [2]string{"—", "gray"}
}

// ---------------------------------------------------------------- currency

// One rates table (base USD) per process, refreshed daily from a free API. Rates are the same for
// every workspace, so they are cached in memory instead of per workspace.
var fx struct {
	sync.Mutex
	day   string
	at    string
	rates map[string]float64
	tried time.Time
}

func fxRates(ctx context.Context) map[string]float64 {
	fx.Lock()
	defer fx.Unlock()
	day := time.Now().UTC().Format("2006-01-02")
	if fx.day == day && fx.rates != nil {
		return fx.rates
	}
	if time.Since(fx.tried) < 10*time.Minute { // API down: do not retry on every page view
		return fx.rates
	}
	fx.tried = time.Now()
	var out struct {
		Rates map[string]float64 `json:"rates"`
	}
	if r, err := netx.GetJSON(ctx, "https://open.er-api.com/v6/latest/USD", nil, &out); err == nil && r.OK() && len(out.Rates) > 0 {
		fx.day, fx.rates, fx.at = day, out.Rates, time.Now().UTC().Format("2006-01-02 15:04")
	} else if err != nil {
		slog.Warn("fx rates", "err", err)
	}
	return fx.rates
}

type fxInfo struct {
	Currency string  `json:"currency"`
	Symbol   string  `json:"symbol"`
	Rate     float64 `json:"rate"`
	Mode     string  `json:"mode"` // auto | manual | fallback | —
	At       string  `json:"at"`
	UsdRate  string  `json:"usd_rate"`
}

// currency returns the USD → workspace currency rate (v1 Usage::fx): automatic (daily, last known rate
// kept in fx_cache) or the manual rate.
func currency(ctx context.Context, q db.Q, s *settings.Store, rates map[string]float64) fxInfo {
	cur := strings.ToUpper(s.Get("currency", "INR"))
	f := fxInfo{Currency: cur, Symbol: s.Get("currency_symbol", "₹"), UsdRate: s.Get("usd_rate", "84")}
	manual := s.Float("usd_rate", 84)
	if cur == "USD" {
		f.Rate, f.Mode, f.Symbol = 1, "—", "$"
		return f
	}
	if s.Get("fx_mode", "auto") == "manual" {
		f.Rate, f.Mode = manual, "manual"
		return f
	}
	var cache struct {
		Cur  string  `json:"cur"`
		Day  string  `json:"day"`
		Rate float64 `json:"rate"`
		At   string  `json:"at"`
	}
	s.JSON("fx_cache", &cache)
	if v, ok := rates[cur]; ok && v > 0 {
		fx.Lock()
		day, at := fx.day, fx.at
		fx.Unlock()
		if cache.Cur != cur || cache.Day != day {
			cache.Cur, cache.Day, cache.Rate, cache.At = cur, day, v, at
			if err := s.Set(ctx, q, "fx_cache", cache); err != nil {
				slog.Warn("fx cache", "err", err)
			}
		}
		f.Rate, f.Mode, f.At = v, "auto", at
		return f
	}
	if cache.Cur == cur && cache.Rate > 0 {
		f.Rate, f.Mode, f.At = cache.Rate, "auto", cache.At
		return f
	}
	f.Rate, f.Mode = manual, "fallback"
	return f
}

// ---------------------------------------------------------------- edit prices

func savePrices(c *app.Ctx) (any, error) {
	var in struct {
		Svc      map[string]SvcPrice   `json:"svc"`
		Models   map[string]ModelPrice `json:"models"`
		Remove   []string              `json:"remove"`
		NewModel *struct {
			ID  string  `json:"id"`
			In  float64 `json:"in"`
			Out float64 `json:"out"`
		} `json:"new_model"`
		FxMode  string   `json:"fx_mode"`
		UsdRate *float64 `json:"usd_rate"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	s := c.S()
	// start from what is saved (only overrides are stored; defaults fill the rest when loading)
	p := LoadPrices(s)
	for k, x := range in.Svc {
		if _, ok := catalogByKey[k]; ok {
			p.Svc[k] = SvcPrice{Price: math.Max(0, x.Price), Free: math.Max(0, x.Free)}
		}
	}
	for k, x := range in.Models {
		if k = strings.TrimSpace(k); k != "" && len(k) <= 120 {
			p.Models[k] = ModelPrice{In: math.Max(0, x.In), Out: math.Max(0, x.Out)}
		}
	}
	for _, k := range in.Remove {
		if _, def := defaultModels[k]; !def {
			delete(p.Models, k)
		}
	}
	if in.NewModel != nil {
		if id := strings.TrimSpace(in.NewModel.ID); id != "" && len(id) <= 120 {
			p.Models[id] = ModelPrice{In: math.Max(0, in.NewModel.In), Out: math.Max(0, in.NewModel.Out)}
		}
	}
	if err := c.SetS(pricesKey, p.encode()); err != nil {
		return nil, err
	}
	mode := "auto"
	if in.FxMode == "manual" {
		mode = "manual"
	}
	if err := c.SetS("fx_mode", mode); err != nil {
		return nil, err
	}
	if in.UsdRate != nil {
		if err := c.SetS("usd_rate", fmt.Sprint(math.Max(0.0001, *in.UsdRate))); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "message": "Prices and limits saved."}, nil
}

// ---------------------------------------------------------------- update prices with AI

const updateKey = "price_update"

type updateState struct {
	Status  string   `json:"status"` // running | done | error
	Msg     string   `json:"msg"`
	Changes []string `json:"changes,omitempty"`
	At      string   `json:"at"`
}

func priceUpdate(s *settings.Store) updateState {
	var u updateState
	s.JSON(updateKey, &u)
	return u
}

func running(u updateState) bool {
	t, err := time.Parse(time.RFC3339, u.At)
	return u.Status == "running" && err == nil && time.Since(t) < 10*time.Minute
}

func setState(ctx context.Context, q db.Q, s *settings.Store, u updateState) error {
	u.At = time.Now().UTC().Format(time.RFC3339)
	return s.Set(ctx, q, updateKey, u)
}

func startAIPrices(c *app.Ctx) (any, error) {
	s := c.S()
	if running(priceUpdate(s)) {
		return nil, app.Bad("A price check is already running.")
	}
	if !ai.Configured(s) {
		return nil, app.Bad("Connect an AI provider first (Settings → AI) – the price check uses your AI connection.")
	}
	tx := c.Tx()
	if _, err := tx.Exec(c, `DELETE FROM jobs WHERE type='prices' AND status='pending'`); err != nil {
		return nil, err
	}
	if _, err := jobs.Push(c, tx, c.Org.ID, "prices", 0, nil, jobs.Opt{MaxAttempts: 1}); err != nil {
		return nil, err
	}
	if err := setState(c, tx, s, updateState{Status: "running", Msg: "Queued – starting in a few seconds…"}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Price check started – the AI is looking up current prices and free limits. This takes 1–3 minutes; you can keep working.", "price_update": priceUpdate(s)}, nil
}

const priceSystem = `You are a cloud API pricing analyst. Check the CURRENT official list prices (USD) on the providers' own pricing pages (use web search when available). Return ONLY one JSON object: {"services":[{"key":"","price":number,"free":number,"source":"https://...","note":"max 80 chars"}],"models":[{"id":"","in":number,"out":number,"source":"https://..."}]}. price = USD per single unit (e.g. 0.035 for $35 per 1,000 requests), free = free units per period (0 if none). Model prices are USD per 1 million input/output tokens at standard (non-batch, non-cached) rates. Include ONLY items whose current price or free limit differs from the values given, and only when you found it on an official page. Never guess.`

// runAIPrices asks the AI (live web search on Claude) for current list prices & free limits and saves
// only sane, changed values. Progress and result are kept in the price_update setting.
func runAIPrices(ctx context.Context, a *app.App, orgID int64) (err error) {
	var s *settings.Store
	if err := a.OrgTx(ctx, orgID, func(tx pgx.Tx, st *settings.Store) error {
		s = st
		return setState(ctx, tx, st, updateState{Status: "running", Msg: "The AI is checking the pricing pages…"})
	}); err != nil {
		return err
	}
	fail := func(msg string) error {
		return a.OrgTx(ctx, orgID, func(tx pgx.Tx, st *settings.Store) error {
			return setState(ctx, tx, st, updateState{Status: "error", Msg: msg})
		})
	}
	defer func() {
		if v := recover(); v != nil {
			slog.Error("prices", "org", orgID, "panic", v)
			err = fail("The price check stopped unexpectedly. Please try again.")
		}
	}()
	P := LoadPrices(s)
	type svcIn struct {
		Key    string  `json:"key"`
		Name   string  `json:"name"`
		Unit   string  `json:"unit"`
		Price  float64 `json:"current_price"`
		Free   float64 `json:"current_free_units"`
		Resets string  `json:"free_resets_every"`
		Page   string  `json:"pricing_page"`
		Notes  string  `json:"notes"`
	}
	var svc []svcIn
	for _, c := range catalog {
		if c.Unit == "tokens" || c.Key == "custom" || c.Key == "voice_custom" || c.Key == "whatsapp_api" || c.Key == "email" {
			continue
		}
		svc = append(svc, svcIn{c.Key, c.Name, "USD per " + unitSingular(c.Unit), P.Svc[c.Key].Price, P.Svc[c.Key].Free, c.Per, c.URL, c.Note})
	}
	type mdlIn struct {
		ID  string  `json:"id"`
		In  float64 `json:"input_per_1M"`
		Out float64 `json:"output_per_1M"`
	}
	var mdl []mdlIn
	for id, x := range P.Models {
		mdl = append(mdl, mdlIn{id, x.In, x.Out})
	}
	sj, _ := json.Marshal(svc)
	mj, _ := json.Marshal(mdl)
	res, aerr := ai.Complete(ctx, a, orgID, s, ai.Request{
		System:    priceSystem,
		Messages:  []ai.Msg{{Role: "user", Content: "Today: " + time.Now().Format("2006-01-02") + "\nServices (keep the same keys):\n" + string(sj) + "\n\nAI models (keep the same ids):\n" + string(mj)}},
		MaxTokens: 6000,
		WebSearch: true,
		JSON:      true,
		Purpose:   "prices",
	})
	if aerr != nil {
		msg := aerr.Error()
		if strings.Contains(msg, "not implemented") {
			msg = "The AI engine is not available yet on this server. Edit the prices below by hand for now."
		}
		return fail(msg)
	}
	var d struct {
		Services []struct {
			Key    string   `json:"key"`
			Price  *float64 `json:"price"`
			Free   *float64 `json:"free"`
			Source string   `json:"source"`
		} `json:"services"`
		Models []struct {
			ID     string   `json:"id"`
			In     *float64 `json:"in"`
			Out    *float64 `json:"out"`
			Source string   `json:"source"`
		} `json:"models"`
	}
	if !parseJSON(res.Text, &d) {
		return fail("The AI answer could not be read. Try again or use a Claude model (it can search the web).")
	}
	return a.OrgTx(ctx, orgID, func(tx pgx.Tx, st *settings.Store) error {
		P := LoadPrices(st) // reload: prices may have been edited meanwhile
		var ch []string
		src := func(u string) string {
			if u != "" {
				return " (" + u + ")"
			}
			return ""
		}
		for _, x := range d.Services {
			c, ok := catalogByKey[x.Key]
			if !ok || x.Price == nil {
				continue
			}
			old := P.Svc[x.Key]
			pr, fr := *x.Price, old.Free
			if x.Free != nil {
				fr = *x.Free
			}
			if pr < 0 || pr > 50 || fr < 0 || fr > 100000000 {
				continue
			}
			if math.Abs(pr-old.Price) < 1e-9 && math.Abs(fr-old.Free) < 1e-6 {
				continue
			}
			P.Svc[x.Key] = SvcPrice{Price: pr, Free: fr}
			line := c.Name + ": $" + trimNum(old.Price, 5) + " → $" + trimNum(pr, 5)
			if fr != old.Free {
				line += ", free " + trimNum(old.Free, 0) + " → " + trimNum(fr, 0)
			}
			ch = append(ch, line+src(x.Source))
		}
		for _, x := range d.Models {
			id := strings.TrimSpace(x.ID)
			old, ok := P.Models[id]
			if !ok || x.In == nil || x.Out == nil {
				continue
			}
			in, out := *x.In, *x.Out
			if in < 0 || in > 500 || out < 0 || out > 2000 {
				continue
			}
			if math.Abs(in-old.In) < 1e-9 && math.Abs(out-old.Out) < 1e-9 {
				continue
			}
			P.Models[id] = ModelPrice{In: in, Out: out}
			ch = append(ch, fmt.Sprintf("%s: $%s / $%s → $%s / $%s per 1M tokens%s", id, trimNum(old.In, 3), trimNum(old.Out, 3), trimNum(in, 3), trimNum(out, 3), src(x.Source)))
		}
		if len(ch) > 0 {
			if err := st.Set(ctx, tx, pricesKey, P.encode()); err != nil {
				return err
			}
		}
		msg := "All prices and free limits are already up to date."
		if len(ch) > 0 {
			msg = fmt.Sprintf("%d price(s) updated from the providers' pricing pages.", len(ch))
		}
		if len(ch) > 40 {
			ch = ch[:40]
		}
		return setState(ctx, tx, st, updateState{Status: "done", Msg: msg, Changes: ch})
	})
}

// parseJSON reads the first JSON object in an AI answer (tolerates code fences and prose around it).
func parseJSON(text string, out any) bool {
	i, j := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}')
	if i < 0 || j <= i {
		return false
	}
	return json.Unmarshal([]byte(text[i:j+1]), out) == nil
}

func trimNum(f float64, dec int) string {
	s := fmt.Sprintf("%.*f", dec, f)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}
