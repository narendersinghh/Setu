package usage

import (
	"encoding/json"
	"strings"

	"github.com/narendersinghh/setu/server/internal/settings"
)

// Service describes one external API Setu can call (v1 Usage::catalog).
type Service struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"`  // search | audit | tokens | message | minute | email | call
	Unit  string  `json:"unit"`  // requests, searches, calls, tokens, messages, minutes, emails
	Price float64 `json:"price"` // USD per unit (not used for tokens)
	Free  float64 `json:"free"`  // free units per period
	Per   string  `json:"per"`   // month | day
	URL   string  `json:"url"`
	Note  string  `json:"note"`
}

var catalog = []Service{
	{"google_places", "Google Places – Text Search (Enterprise)", "search", "requests", 0.035, 1000, "month", "https://developers.google.com/maps/billing-and-pricing/pricing", `1 request = 1 result page of up to 20 businesses (20 dentists = 1 request, 60 = 3). Billed per request – not per business or per field – at the highest SKU of the fields asked for. Phone, website, rating & hours make it "Text Search Enterprise": $35 per 1,000 after 1,000 free requests a month.`},
	{"google_places_atmo", "Google Places – Text Search (Enterprise + Atmosphere)", "search", "requests", 0.040, 1000, "month", "https://developers.google.com/maps/billing-and-pricing/pricing", `Used only when "Fetch business descriptions" is on (adds the editorial summary): $40 per 1,000 after 1,000 free requests a month.`},
	{"google_places_ids", "Google Places – duplicate check (IDs only)", "search", "requests", 0, 0, "month", "https://developers.google.com/maps/billing-and-pricing/pricing", `Text Search asking only for place IDs is free and unlimited (Essentials IDs only). Setu uses it before every page so businesses already in your data are never paid for twice.`},
	{"google_places_details", "Google Places – Place Details (Enterprise)", "search", "requests", 0.020, 1000, "month", "https://developers.google.com/maps/billing-and-pricing/pricing", `1 request = full details of 1 business. Used instead of a whole result page when only 1 new business is left on a page (cheaper). $20 per 1,000 after 1,000 free a month.`},
	{"google_places_details_atmo", "Google Places – Place Details (Enterprise + Atmosphere)", "search", "requests", 0.025, 1000, "month", "https://developers.google.com/maps/billing-and-pricing/pricing", `Place Details with business descriptions: $25 per 1,000 after 1,000 free a month.`},
	{"serpapi", "SerpAPI – Google Maps", "search", "searches", 0.015, 250, "month", "https://serpapi.com/pricing", `Each page of 20 results = 1 search. Free plan: 250 searches/month; paid plans from about $0.015 per search.`},
	{"pagespeed", "Google PageSpeed Insights", "audit", "calls", 0, 25000, "day", "https://developers.google.com/speed/docs/insights/v5/get-started", `Free. Default quota 25,000 calls per day.`},
	{"anthropic", "Anthropic Claude", "tokens", "tokens", 0, 0, "month", "https://www.anthropic.com/pricing", `Billed per million input / output tokens (prices below). One report ≈ 10k input + 5k output tokens.`},
	{"web_search", "Claude web research", "search", "searches", 0.01, 0, "month", "https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/web-search-tool", `Optional live research while writing reports: $10 per 1,000 searches (about 2–4 per report).`},
	{"openai", "OpenAI", "tokens", "tokens", 0, 0, "month", "https://openai.com/api/pricing", ``},
	{"openrouter", "OpenRouter", "tokens", "tokens", 0, 0, "month", "https://openrouter.ai/models", ``},
	{"gemini", "Google Gemini", "tokens", "tokens", 0, 0, "month", "https://ai.google.dev/pricing", `Free tier with rate limits is available.`},
	{"groq", "Groq", "tokens", "tokens", 0, 0, "month", "https://groq.com/pricing", ``},
	{"deepseek", "DeepSeek", "tokens", "tokens", 0, 0, "month", "https://api-docs.deepseek.com/quick_start/pricing", ``},
	{"custom", "Custom AI", "tokens", "tokens", 0, 0, "month", "", ``},
	{"whatsapp", "WhatsApp Cloud API", "message", "messages", 0.0107, 0, "month", "https://developers.facebook.com/docs/whatsapp/pricing", `Business-initiated (marketing/utility template) messages are charged per message; India marketing ≈ ₹0.88. Replies within 24h of a customer message are free.`},
	{"whatsapp_api", "WhatsApp (other provider)", "message", "messages", 0, 0, "month", "", `Check your provider's plan.`},
	{"voice_sarvam", "Sarvam AI voice", "minute", "minutes", 0.06, 0, "month", "https://www.sarvam.ai/pricing", `Charged per call minute – set your plan's rate.`},
	{"voice_bland", "Bland AI voice", "minute", "minutes", 0.09, 0, "month", "https://www.bland.ai/pricing", ``},
	{"voice_vapi", "Vapi voice", "minute", "minutes", 0.05, 0, "month", "https://vapi.ai/pricing", `Platform fee; model, voice and telephony are billed on top.`},
	{"voice_retell", "Retell AI voice", "minute", "minutes", 0.07, 0, "month", "https://www.retellai.com/pricing", ``},
	{"voice_custom", "Other voice API", "minute", "minutes", 0, 0, "month", "", ``},
	{"email", "Email sending", "email", "emails", 0, 0, "month", "", `Free with your hosting mailbox; hosts usually limit to a few hundred emails per hour.`},
	{"gcal", "Google Calendar API", "call", "calls", 0, 1000000, "day", "https://developers.google.com/calendar/api/guides/quota", `Free (generous daily quota).`},
}

var catalogByKey = func() map[string]Service {
	m := make(map[string]Service, len(catalog))
	for _, s := range catalog {
		m[s.Key] = s
	}
	return m
}()

// Catalog returns every known service.
func Catalog() []Service { return catalog }

// groups for the usage page (same order and icons as v1)
var groups = []struct {
	Name, Ic, C string
	Keys        []string
}{
	{"Maps & website data", "map", "orange", []string{"google_places", "google_places_details", "google_places_ids", "google_places_atmo", "google_places_details_atmo", "serpapi", "pagespeed"}},
	{"AI writing", "spark", "violet", []string{"anthropic", "web_search", "openai", "openrouter", "gemini", "groq", "deepseek", "custom"}},
	{"Messaging", "msg", "green", []string{"email", "whatsapp", "whatsapp_api"}},
	{"AI voice calls", "mic", "pink", []string{"voice_sarvam", "voice_bland", "voice_vapi", "voice_retell", "voice_custom"}},
	{"Calendar", "cal", "teal", []string{"gcal"}},
}

// Default model prices in USD per 1M tokens [input, output]; editable on the usage page.
var defaultModels = map[string][2]float64{
	"claude-opus-5-5": {4, 20}, "claude-opus-5": {5, 25}, "claude-sonnet-5-5": {2, 10}, "claude-sonnet-5": {2, 10},
	"claude-haiku-5-5": {0.10, 0.50}, "claude-fable-5-1": {10, 50}, "gpt-4.1-mini": {0.40, 1.60}, "gpt-4.1": {2, 8},
	"gpt-4o": {2.50, 10}, "gemini-2.5-flash": {0.30, 2.50}, "gemini-2.5-pro": {1.25, 10}, "deepseek-chat": {0.27, 1.10},
	"llama-3.3-70b-versatile": {0.59, 0.79},
}

type SvcPrice struct {
	Price float64 `json:"price"`
	Free  float64 `json:"free"`
}

type ModelPrice struct {
	In  float64 `json:"in"`
	Out float64 `json:"out"`
}

// Prices are the workspace's prices: saved overrides (setting usage_prices) over the defaults.
type Prices struct {
	Svc    map[string]SvcPrice
	Models map[string]ModelPrice
}

const pricesKey = "usage_prices"

// LoadPrices merges the saved prices with the catalog defaults.
func LoadPrices(s *settings.Store) Prices {
	raw := map[string]json.RawMessage{}
	s.JSON(pricesKey, &raw)
	p := Prices{Svc: map[string]SvcPrice{}, Models: map[string]ModelPrice{}}
	for k, v := range raw {
		if k == "_models" {
			json.Unmarshal(v, &p.Models)
			continue
		}
		var x SvcPrice
		if json.Unmarshal(v, &x) == nil {
			p.Svc[k] = x
		}
	}
	if p.Models == nil {
		p.Models = map[string]ModelPrice{}
	}
	for _, c := range catalog {
		if _, ok := p.Svc[c.Key]; !ok {
			p.Svc[c.Key] = SvcPrice{Price: c.Price, Free: c.Free}
		}
	}
	for id, m := range defaultModels {
		if _, ok := p.Models[id]; !ok {
			p.Models[id] = ModelPrice{In: m[0], Out: m[1]}
		}
	}
	return p
}

// encode returns the JSON stored in the usage_prices setting (v1 format: {service:{price,free}, _models:{id:{in,out}}}).
func (p Prices) encode() string {
	o := map[string]any{}
	for k, v := range p.Svc {
		o[k] = v
	}
	o["_models"] = p.Models
	b, _ := json.Marshal(o)
	return string(b)
}

func (p Prices) model(model string) (ModelPrice, bool) {
	if m, ok := p.Models[model]; ok {
		return m, true
	}
	if i := strings.LastIndexByte(model, '/'); i >= 0 {
		m, ok := p.Models[model[i+1:]]
		return m, ok
	}
	return ModelPrice{}, false
}

// AIPrice is the price of an AI model in USD per 1M input / output tokens (0, 0 when unknown).
// "provider/model" ids (OpenRouter) fall back to the part after the last slash.
func AIPrice(s *settings.Store, model string) (inPerM, outPerM float64) {
	m, _ := LoadPrices(s).model(model)
	return m.In, m.Out
}

// Cost estimates the USD cost of non-token API calls (v1 Usage::log): per call, per unit, or per
// minute for voice (unitsIn = seconds). Token services return 0 – use AIPrice.
func Cost(s *settings.Store, service string, calls, unitsIn int64) float64 {
	c, ok := catalogByKey[service]
	if !ok || c.Unit == "tokens" {
		return 0
	}
	p := LoadPrices(s).Svc[service]
	switch {
	case c.Unit == "minutes":
		return float64(unitsIn) / 60 * p.Price
	case unitsIn > 0:
		return float64(unitsIn) * p.Price
	}
	return float64(calls) * p.Price
}

func unitSingular(u string) string {
	switch u {
	case "searches":
		return "search"
	case "calls":
		return "call"
	case "requests":
		return "request"
	case "messages":
		return "message"
	case "minutes":
		return "minute"
	case "emails":
		return "email"
	}
	return u
}
