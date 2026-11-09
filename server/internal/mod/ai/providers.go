package ai

import (
	"errors"
	"regexp"
	"strings"

	"github.com/narendersinghh/setu/server/internal/settings"
)

// Provider describes one AI service on the AI connection page (same texts as v1 AI::providers).
type Provider struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Models  string   `json:"models"`
	Steps   []string `json:"steps"`
	Note    string   `json:"note"`
	Suggest []string `json:"suggest"`
}

// Providers in the order shown on the AI connection page.
var Providers = []Provider{
	{"anthropic", "Anthropic Claude", "https://console.anthropic.com/settings/keys", "claude-opus-5-5, claude-sonnet-5-5, claude-haiku-5-5",
		[]string{"Go to console.anthropic.com and sign in (or create an account).", "Open Settings → Billing and add credits.", "Open Settings → API Keys → \"Create Key\". Copy the key (starts with sk-ant-).", "Paste it below, choose a model and click Save, then \"Test connection\"."},
		"Recommended. Best report writing quality. claude-opus-5-5 = best quality, claude-haiku-5-5 = cheapest.",
		[]string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}},
	{"openai", "OpenAI ChatGPT", "https://platform.openai.com/api-keys", "gpt-4.1-mini, gpt-4.1, gpt-4o",
		[]string{"Go to platform.openai.com and sign in.", "Open Settings → Billing and add credits (ChatGPT Plus does NOT include API usage).", "Open API keys → \"Create new secret key\". Copy it (starts with sk-).", "Paste it below with a model name, Save and Test."},
		"Use any chat model available on your OpenAI account.",
		[]string{"gpt-4.1", "gpt-4.1-mini", "gpt-4o"}},
	{"openrouter", "OpenRouter", "https://openrouter.ai/keys", "openrouter/auto, anthropic/…, openai/…, google/…",
		[]string{"Go to openrouter.ai and sign in.", "Add credits under Credits.", "Open Keys → \"Create Key\" and copy it (starts with sk-or-).", "Paste below. Model ids look like \"anthropic/claude-…\" – see openrouter.ai/models."},
		"One key, hundreds of models (Claude, GPT, Gemini, Llama…).",
		[]string{"openrouter/auto", "anthropic/claude-sonnet-5-5", "openai/gpt-4.1"}},
	{"gemini", "Google Gemini", "https://aistudio.google.com/app/apikey", "gemini-2.5-flash, gemini-2.5-pro",
		[]string{"Go to aistudio.google.com and sign in with Google.", "Click \"Get API key\" → \"Create API key\".", "Copy the key and paste it below with a model name."},
		"Has a free tier with rate limits.",
		[]string{"gemini-2.5-pro", "gemini-2.5-flash"}},
	{"groq", "Groq", "https://console.groq.com/keys", "llama-3.3-70b-versatile",
		[]string{"Go to console.groq.com and sign in.", "Open API Keys → Create API Key.", "Paste it below with a model name."},
		"Very fast open-source models.",
		[]string{"llama-3.3-70b-versatile"}},
	{"deepseek", "DeepSeek", "https://platform.deepseek.com/api_keys", "deepseek-chat",
		[]string{"Go to platform.deepseek.com and sign in.", "Top up balance, then create an API key.", "Paste it below."},
		"Low-cost option.",
		[]string{"deepseek-chat"}},
	{"custom", "Custom (OpenAI-compatible)", "", "any",
		[]string{"Use any provider with an OpenAI-compatible /chat/completions endpoint (Together, Mistral, Azure OpenAI proxy, self-hosted…).", "Enter the full base URL, e.g. https://api.together.xyz/v1", "Enter the key and model name."},
		"For advanced users.",
		[]string{}},
}

func providerByKey(k string) (Provider, bool) {
	for _, p := range Providers {
		if p.Key == k {
			return p, true
		}
	}
	return Provider{}, false
}

// ProviderName is the display name of a provider key.
func ProviderName(k string) string {
	if p, ok := providerByKey(k); ok {
		return p.Name
	}
	return k
}

// OpenAI-compatible chat endpoints (v1 AI::bases).
var openAIBases = map[string]string{
	"openai":     "https://api.openai.com/v1",
	"openrouter": "https://openrouter.ai/api/v1",
	"groq":       "https://api.groq.com/openai/v1",
	"deepseek":   "https://api.deepseek.com/v1",
}

const (
	anthropicBase    = "https://api.anthropic.com"
	geminiBase       = "https://generativelanguage.googleapis.com/v1beta"
	anthropicVersion = "2026-10-21"
)

// Setting keys (same as v1).
const (
	SetProvider    = "ai_provider"     // active provider key (default anthropic)
	SetKeyPrefix   = "ai_key_"         // ai_key_<provider>  (secret)
	SetModelPrefix = "ai_model_"       // ai_model_<provider>
	SetCustomURL   = "ai_custom_url"   // base URL of the custom OpenAI-compatible endpoint
	SetAnthropic   = "anthropic_base"  // optional proxy / gateway for the Claude API
	SetResearch    = "ai_web_research" // market research step for reports (1/0, default 1)
	SetBulk        = "ai_batch"        // bulk reports: parallel | batch | off
	SetParallel    = "ai_parallel"     // requests at the same time (1–10, default 5)
)

// conn is a resolved AI connection.
type conn struct {
	Provider string
	Key      string
	Model    string
	Base     string // API address without trailing slash
	Guard    bool   // address typed by the user: SSRF guard on
	Referer  string // OpenRouter attribution
}

// ActiveProvider is the workspace's provider key.
func ActiveProvider(s *settings.Store) string {
	if s == nil {
		return "anthropic"
	}
	return s.Get(SetProvider, "anthropic")
}

// Model returns the configured model of a provider ("" = active provider).
func Model(s *settings.Store, p string) string {
	if p == "" {
		p = ActiveProvider(s)
	}
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.Get(SetModelPrefix+p, ""))
}

// connFor resolves provider, key, model and API address from the settings.
func connFor(s *settings.Store, p, appURL string) (conn, error) {
	if p == "" {
		p = ActiveProvider(s)
	}
	pv, ok := providerByKey(p)
	if !ok {
		return conn{}, errors.New("Unknown AI provider.")
	}
	c := conn{Provider: p, Key: strings.TrimSpace(s.Get(SetKeyPrefix+p, "")), Model: Model(s, p), Referer: appURL}
	if c.Key == "" {
		return c, errors.New("No API key for " + pv.Name + ". Add it in the AI Connection tab.")
	}
	if c.Model == "" {
		return c, errors.New("No AI model selected in the AI Connection tab.")
	}
	switch p {
	case "anthropic":
		c.Base = strings.TrimRight(s.Get(SetAnthropic, anthropicBase), "/")
		c.Guard = c.Base != anthropicBase
	case "gemini":
		c.Base = geminiBase
	case "custom":
		c.Base = strings.TrimRight(strings.TrimSpace(s.Get(SetCustomURL, "")), "/")
		c.Guard = true
		if c.Base == "" {
			return c, errors.New("Enter the base URL of your custom AI endpoint in the AI Connection tab.")
		}
	default:
		c.Base = openAIBases[p]
	}
	return c, nil
}

// ---------------------------------------------------------------- prices

// Model prices in USD per 1M tokens [input, output] (v1 Usage::modelPrices). Overridable per workspace
// in the setting usage_prices {"_models":{"model":{"in":x,"out":y}}} (edited on the usage page).
// TODO(usage): switch to usage.AIPrice(s, model) once the usage module exports it.
var modelPrices = map[string][2]float64{
	"claude-opus-5-5": {4, 20}, "claude-opus-5": {5, 25}, "claude-sonnet-5-5": {2, 10}, "claude-sonnet-5": {2, 10},
	"claude-haiku-5-5": {0.10, 0.50}, "claude-fable-5-1": {10, 50},
	"gpt-4.1-mini": {0.40, 1.60}, "gpt-4.1": {2, 8}, "gpt-4o": {2.50, 10},
	"gemini-2.5-flash": {0.30, 2.50}, "gemini-2.5-pro": {1.25, 10},
	"deepseek-chat": {0.27, 1.10}, "llama-3.3-70b-versatile": {0.59, 0.79},
}

// webSearchPrice: Claude web search, USD per search ($10 per 1,000).
const webSearchPrice = 0.01

type priceOverride struct {
	Models map[string]struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"_models"`
	WebSearch struct {
		Price *float64 `json:"price"`
	} `json:"web_search"`
}

func overrides(s *settings.Store) priceOverride {
	var o priceOverride
	if s != nil {
		s.JSON("usage_prices", &o)
	}
	return o
}

// Price returns the USD price per 1M input / output tokens of a model.
func Price(s *settings.Store, model string) (in, out float64, ok bool) {
	o := overrides(s)
	short := model
	if i := strings.LastIndexByte(model, '/'); i >= 0 {
		short = model[i+1:]
	}
	for _, m := range []string{model, short} {
		if p, found := o.Models[m]; found {
			return p.In, p.Out, true
		}
		if p, found := modelPrices[m]; found {
			return p[0], p[1], true
		}
	}
	return 0, 0, false
}

// Cost of one call in USD (mult 0.5 for Message Batches).
func Cost(s *settings.Store, model string, in, out int, mult float64) float64 {
	pi, po, ok := Price(s, model)
	if !ok {
		return 0
	}
	if mult == 0 {
		mult = 1
	}
	return (float64(in)/1e6*pi + float64(out)/1e6*po) * mult
}

func searchCost(s *settings.Store, n int) float64 {
	if o := overrides(s); o.WebSearch.Price != nil {
		return float64(n) * *o.WebSearch.Price
	}
	return float64(n) * webSearchPrice
}

// ---------------------------------------------------------------- Claude model capabilities (same rules as v1)

var (
	reEffort    = regexp.MustCompile(`^claude-(opus|sonnet|haiku|fable)-5`)
	reNewSearch = regexp.MustCompile(`^claude-(opus|sonnet|fable)-|^claude-sonnet-4-6|^claude-opus-4-[678]`)
	reFallback  = regexp.MustCompile(`^claude-(opus-5|opus-5-5|sonnet-5-5|fable-5-1)$`)
)

func webSearchTool(model string, maxUses int) map[string]any {
	t := "web_search_20250305"
	if reNewSearch.MatchString(model) {
		t = "web_search_20260209"
	}
	if maxUses <= 0 {
		maxUses = 4
	}
	return map[string]any{"type": t, "name": "web_search", "max_uses": maxUses}
}
