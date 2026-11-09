package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/netx"
)

// Plan purchases: Razorpay Payment Links and Stripe Checkout (hosted pages – card data never touches
// this server). A paid purchase extends the organisation's plan by one month or one year.

func registerBilling(a *app.App) {
	a.Get("/billing", billingGet, app.Perm("settings"))
	a.Post("/billing/checkout", billingCheckout, app.Perm("settings"))
	a.Public(http.MethodGet, "/billing/return/razorpay", razorpayReturn)
	a.Public(http.MethodPost, "/webhooks/billing/razorpay", razorpayWebhook)
	a.Public(http.MethodPost, "/webhooks/billing/stripe", stripeWebhook)
	p := app.PlatformOnly()
	a.Get("/platform/payments/settings", paySettingsGet, p)
	a.Put("/platform/payments/settings", paySettingsPut, p)
	a.Get("/platform/payments", paymentsList, p)
}

var payKeys = []string{"razorpay_enabled", "razorpay_key_id", "razorpay_key_secret", "razorpay_webhook_secret", "stripe_enabled", "stripe_secret_key", "stripe_webhook_secret"}

type gateway struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Gateways returns the enabled payment gateways.
func Gateways(ctx context.Context, a *app.App) []gateway {
	m := PSettings(ctx, a)
	out := []gateway{}
	if m["razorpay_enabled"] == "1" && m["razorpay_key_id"] != "" && m["razorpay_key_secret"] != "" {
		out = append(out, gateway{"razorpay", "Razorpay – UPI, cards, net banking, wallets"})
	}
	if m["stripe_enabled"] == "1" && m["stripe_secret_key"] != "" {
		out = append(out, gateway{"stripe", "Stripe – international cards"})
	}
	return out
}

func paySettingsGet(c *app.Ctx) (any, error) {
	out := masked(PSettings(c, c.A), payKeys...)
	base := c.A.Cfg.AppURL()
	out["webhooks"] = map[string]string{"razorpay": base + "/webhooks/billing/razorpay", "stripe": base + "/webhooks/billing/stripe"}
	out["razorpay_events"] = "payment_link.paid"
	out["stripe_events"] = "checkout.session.completed"
	return out, nil
}

func paySettingsPut(c *app.Ctx) (any, error) {
	var in map[string]any
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	kv := map[string]string{}
	for _, k := range payKeys {
		switch v := in[k].(type) {
		case string:
			kv[k] = strings.TrimSpace(v)
		case bool:
			kv[k] = map[bool]string{true: "1", false: "0"}[v]
		}
	}
	if err := PSet(c, c.A, kv); err != nil {
		return nil, err
	}
	return paySettingsGet(c)
}

func paymentsList(c *app.Ctx) (any, error) {
	return db.Maps(c, c.A.DB.Pool, `SELECT y.id,y.org_id,o.name org,COALESCE(p.name,'') plan,y.cycle,y.gateway,y.amount::float8 amount,y.currency,y.status,y.ext_id,y.created_at,y.paid_at,COALESCE(u.email,'') by_email
FROM payments y JOIN orgs o ON o.id=y.org_id LEFT JOIN plans p ON p.id=y.plan_id LEFT JOIN users u ON u.id=y.user_id
ORDER BY y.id DESC LIMIT 500`)
}

// ---------------------------------------------------------------- organisation side

func billingGet(c *app.Ctx) (any, error) {
	if sid := c.Query("session_id"); sid != "" { // back from Stripe: confirm without waiting for the webhook
		confirmStripeSession(c, c.A, sid)
	}
	var o struct {
		PlanID   *int64     `db:"plan_id"`
		Trial    *time.Time `db:"trial_ends_at"`
		Expires  *time.Time `db:"plan_expires_at"`
		Status   string     `db:"plan_status"`
		Name     string     `db:"name"`
		PM       float64    `db:"price_month"`
		PY       float64    `db:"price_year"`
		Currency string     `db:"currency"`
	}
	err := c.A.DB.Pool.QueryRow(c, `SELECT o.plan_id,o.trial_ends_at,o.plan_expires_at,`+app.PlanStatusSQL+` plan_status,
 COALESCE(p0.name,'') name, COALESCE(p0.price_month,0)::float8 price_month, COALESCE(p0.price_year,0)::float8 price_year, COALESCE(p0.currency,'INR') currency
FROM orgs o LEFT JOIN plans p0 ON p0.id=o.plan_id WHERE o.id=$1`, c.Org.ID).Scan(&o.PlanID, &o.Trial, &o.Expires, &o.Status, &o.Name, &o.PM, &o.PY, &o.Currency)
	if err != nil {
		return nil, err
	}
	limits := []map[string]any{}
	for _, k := range app.LimitKeys {
		limits = append(limits, map[string]any{"key": k[0], "label": k[1], "limit": c.Org.Limits[k[0]], "used": app.Usage(c, c.Tx(), k[0])})
	}
	plans, err := db.All[Plan](c, c.A.DB.Pool, `SELECT `+planCols+` FROM plans p WHERE p.public ORDER BY p.sort, p.price_month`)
	if err != nil {
		return nil, err
	}
	pays, err := db.Maps(c, c.A.DB.Pool, `SELECT y.id,COALESCE(p.name,'') plan,y.cycle,y.gateway,y.amount::float8 amount,y.currency,y.status,y.created_at,y.paid_at
FROM payments y LEFT JOIN plans p ON p.id=y.plan_id WHERE y.org_id=$1 ORDER BY y.id DESC LIMIT 50`, c.Org.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"plan":   map[string]any{"id": o.PlanID, "name": o.Name, "price_month": o.PM, "price_year": o.PY, "currency": o.Currency},
		"status": o.Status, "trial_ends_at": o.Trial, "expires_at": o.Expires, "effective_plan": c.Org.Plan,
		"limits": limits, "plans": plans, "gateways": Gateways(c, c.A), "payments": pays, "can_pay": c.IsSuper() || c.Can("settings"),
	}, nil
}

func billingCheckout(c *app.Ctx) (any, error) {
	var in struct {
		PlanID  int64  `json:"plan_id"`
		Cycle   string `json:"cycle"`
		Gateway string `json:"gateway"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if in.Cycle != "year" {
		in.Cycle = "month"
	}
	ok := false
	for _, g := range Gateways(c, c.A) {
		ok = ok || g.Key == in.Gateway
	}
	if !ok {
		return nil, app.Bad("Online payment is not available. Contact us to change your plan.")
	}
	p, err := db.One[Plan](c, c.A.DB.Pool, `SELECT `+planCols+` FROM plans p WHERE p.id=$1 AND p.public`, in.PlanID)
	if err != nil {
		return nil, app.Bad("Choose a plan.")
	}
	amount := p.PriceMonth
	if in.Cycle == "year" {
		amount = p.PriceYear
	}
	if amount <= 0 {
		return nil, app.Bad("This plan has no price for that billing period.")
	}
	var payID int64
	if err := c.A.DB.Pool.QueryRow(c, `INSERT INTO payments (org_id,plan_id,user_id,cycle,gateway,amount,currency) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		c.Org.ID, p.ID, c.User.ID, in.Cycle, in.Gateway, amount, p.Currency).Scan(&payID); err != nil {
		return nil, err
	}
	desc := fmt.Sprintf("%s – %s plan (%s)", Brand(c, c.A)["name"], p.Name, map[string]string{"month": "1 month", "year": "1 year"}[in.Cycle])
	var extID, payURL string
	switch in.Gateway {
	case "razorpay":
		extID, payURL, err = razorpayLink(c, c.A, payID, amount, p.Currency, desc, c.User.Name, c.User.Email)
	case "stripe":
		extID, payURL, err = stripeSession(c, c.A, payID, amount, p.Currency, desc, c.User.Email)
	}
	if err != nil {
		c.A.DB.Pool.Exec(c, `UPDATE payments SET status='failed' WHERE id=$1`, payID)
		slog.Warn("checkout", "gateway", in.Gateway, "err", err)
		return nil, app.Bad("The payment page could not be opened: " + err.Error())
	}
	c.A.DB.Pool.Exec(c, `UPDATE payments SET ext_id=$2 WHERE id=$1`, payID, extID)
	return map[string]any{"url": payURL}, nil
}

func minor(amount float64) int64 { return int64(math.Round(amount * 100)) }

// ---------------------------------------------------------------- Razorpay (Payment Links)

func razorpayLink(ctx context.Context, a *app.App, payID int64, amount float64, cur, desc, name, email string) (string, string, error) {
	m := PSettings(ctx, a)
	body := map[string]any{
		"amount": minor(amount), "currency": strings.ToUpper(cur), "description": desc, "reference_id": "setu-" + strconv.FormatInt(payID, 10),
		"customer": map[string]any{"name": name, "email": email}, "notify": map[string]any{"email": true},
		"callback_url": a.Cfg.AppURL() + "/billing/return/razorpay", "callback_method": "get",
		"notes": map[string]any{"payment_id": strconv.FormatInt(payID, 10)},
	}
	r, err := netx.Do(ctx, netx.Req{Method: http.MethodPost, URL: "https://api.razorpay.com/v1/payment_links", JSON: body,
		Headers: map[string]string{"Authorization": "Basic " + basic(m["razorpay_key_id"], m["razorpay_key_secret"])}})
	if err != nil {
		return "", "", err
	}
	var out struct {
		ID       string `json:"id"`
		ShortURL string `json:"short_url"`
		Error    struct {
			Description string `json:"description"`
		} `json:"error"`
	}
	r.JSON(&out)
	if !r.OK() || out.ShortURL == "" {
		return "", "", fmt.Errorf("Razorpay: %s", firstNonEmpty(out.Error.Description, "HTTP "+strconv.Itoa(r.Code)))
	}
	return out.ID, out.ShortURL, nil
}

func basic(u, p string) string {
	return base64Std(u + ":" + p)
}

func hmacHex(key, msg string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

// razorpayReturn: the customer comes back from the payment page (signature checked with the key secret).
func razorpayReturn(c *app.Ctx) (any, error) {
	c.Raw()
	q := c.R.URL.Query()
	link, ref, st, pay, sig := q.Get("razorpay_payment_link_id"), q.Get("razorpay_payment_link_reference_id"), q.Get("razorpay_payment_link_status"), q.Get("razorpay_payment_id"), q.Get("razorpay_signature")
	secret := PSettings(c, c.A)["razorpay_key_secret"]
	dest := "/billing?failed=1"
	if secret != "" && hmac.Equal([]byte(hmacHex(secret, link+"|"+ref+"|"+st+"|"+pay)), []byte(sig)) && st == "paid" {
		if err := markPaid(c, c.A, "razorpay", link, pay); err == nil {
			dest = "/billing?paid=1"
		}
	}
	http.Redirect(c.W, c.R, dest, http.StatusSeeOther)
	return nil, nil
}

func razorpayWebhook(c *app.Ctx) (any, error) {
	body, err := io.ReadAll(io.LimitReader(c.R.Body, 1<<20))
	if err != nil {
		return nil, app.Bad("bad body")
	}
	secret := PSettings(c, c.A)["razorpay_webhook_secret"]
	if secret == "" || !hmac.Equal([]byte(hmacHex(secret, string(body))), []byte(c.R.Header.Get("X-Razorpay-Signature"))) {
		return nil, app.Err(http.StatusUnauthorized, "bad signature")
	}
	var ev struct {
		Event   string `json:"event"`
		Payload struct {
			PaymentLink struct {
				Entity struct {
					ID string `json:"id"`
				} `json:"entity"`
			} `json:"payment_link"`
			Payment struct {
				Entity struct {
					ID string `json:"id"`
				} `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}
	json.Unmarshal(body, &ev)
	if ev.Event == "payment_link.paid" {
		if err := markPaid(c, c.A, "razorpay", ev.Payload.PaymentLink.Entity.ID, ev.Payload.Payment.Entity.ID); err != nil {
			slog.Warn("razorpay webhook", "err", err)
		}
	}
	return map[string]any{"ok": true}, nil
}

// ---------------------------------------------------------------- Stripe (Checkout)

func stripeSession(ctx context.Context, a *app.App, payID int64, amount float64, cur, desc, email string) (string, string, error) {
	base := a.Cfg.AppURL()
	f := url.Values{}
	f.Set("mode", "payment")
	f.Set("line_items[0][quantity]", "1")
	f.Set("line_items[0][price_data][currency]", strings.ToLower(cur))
	f.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(minor(amount), 10))
	f.Set("line_items[0][price_data][product_data][name]", desc)
	f.Set("success_url", base+"/billing?paid=1&session_id={CHECKOUT_SESSION_ID}")
	f.Set("cancel_url", base+"/billing?failed=1")
	f.Set("client_reference_id", strconv.FormatInt(payID, 10))
	f.Set("metadata[payment_id]", strconv.FormatInt(payID, 10))
	if email != "" {
		f.Set("customer_email", email)
	}
	r, err := stripeCall(ctx, a, http.MethodPost, "/v1/checkout/sessions", f)
	if err != nil {
		return "", "", err
	}
	var out struct {
		ID    string `json:"id"`
		URL   string `json:"url"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	r.JSON(&out)
	if !r.OK() || out.URL == "" {
		return "", "", fmt.Errorf("Stripe: %s", firstNonEmpty(out.Error.Message, "HTTP "+strconv.Itoa(r.Code)))
	}
	return out.ID, out.URL, nil
}

func stripeCall(ctx context.Context, a *app.App, method, path string, f url.Values) (*netx.Resp, error) {
	req := netx.Req{Method: method, URL: "https://api.stripe.com" + path, Headers: map[string]string{"Authorization": "Bearer " + PSettings(ctx, a)["stripe_secret_key"]}}
	if f != nil {
		req.Body = strings.NewReader(f.Encode())
		req.Headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	return netx.Do(ctx, req)
}

func confirmStripeSession(ctx context.Context, a *app.App, sid string) {
	if !strings.HasPrefix(sid, "cs_") || len(sid) > 200 {
		return
	}
	r, err := stripeCall(ctx, a, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(sid), nil)
	if err != nil || !r.OK() {
		return
	}
	var s struct {
		PaymentStatus string `json:"payment_status"`
		PaymentIntent string `json:"payment_intent"`
	}
	if r.JSON(&s) == nil && s.PaymentStatus == "paid" {
		markPaid(ctx, a, "stripe", sid, s.PaymentIntent)
	}
}

func stripeWebhook(c *app.Ctx) (any, error) {
	body, err := io.ReadAll(io.LimitReader(c.R.Body, 1<<20))
	if err != nil {
		return nil, app.Bad("bad body")
	}
	secret := PSettings(c, c.A)["stripe_webhook_secret"]
	if secret == "" || !stripeSigOK(secret, c.R.Header.Get("Stripe-Signature"), body, time.Now()) {
		return nil, app.Err(http.StatusUnauthorized, "bad signature")
	}
	var ev struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID            string `json:"id"`
				PaymentStatus string `json:"payment_status"`
				PaymentIntent string `json:"payment_intent"`
			} `json:"object"`
		} `json:"data"`
	}
	json.Unmarshal(body, &ev)
	if (ev.Type == "checkout.session.completed" || ev.Type == "checkout.session.async_payment_succeeded") && ev.Data.Object.PaymentStatus == "paid" {
		if err := markPaid(c, c.A, "stripe", ev.Data.Object.ID, ev.Data.Object.PaymentIntent); err != nil {
			slog.Warn("stripe webhook", "err", err)
		}
	}
	return map[string]any{"received": true}, nil
}

// stripeSigOK verifies the Stripe-Signature header (t=…,v1=…) with a 5-minute tolerance.
func stripeSigOK(secret, header string, body []byte, now time.Time) bool {
	var t string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			t = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	ts, err := strconv.ParseInt(t, 10, 64)
	if err != nil || math.Abs(float64(now.Unix()-ts)) > 300 {
		return false
	}
	want := hmacHex(secret, t+"."+string(body))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- activation

// markPaid records a paid purchase once and extends the organisation's plan (idempotent).
func markPaid(ctx context.Context, a *app.App, gw, extID, extPayment string) error {
	if extID == "" {
		return fmt.Errorf("missing reference")
	}
	err := pgx.BeginFunc(ctx, a.DB.Pool, func(tx pgx.Tx) error {
		var org, plan int64
		var cycle string
		err := tx.QueryRow(ctx, `UPDATE payments SET status='paid', paid_at=now(), ext_payment=$3 WHERE gateway=$1 AND ext_id=$2 AND status<>'paid' RETURNING org_id, plan_id, cycle`, gw, extID, extPayment).Scan(&org, &plan, &cycle)
		if err == pgx.ErrNoRows {
			return nil // already processed or unknown
		}
		if err != nil {
			return err
		}
		iv := "1 month"
		if cycle == "year" {
			iv = "1 year"
		}
		_, err = tx.Exec(ctx, `UPDATE orgs SET plan_id=$2, trial_ends_at=NULL,
 plan_expires_at = GREATEST(CASE WHEN plan_id=$2 THEN COALESCE(plan_expires_at, now()) ELSE now() END, now()) + $3::interval WHERE id=$1`, org, plan, iv)
		if err == nil {
			slog.Info("plan purchased", "org", org, "plan", plan, "cycle", cycle, "gateway", gw)
		}
		return err
	})
	if err == nil {
		a.ForgetAllOrgs()
	}
	return err
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func base64Std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
