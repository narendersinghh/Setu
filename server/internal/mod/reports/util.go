package reports

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

// J is a loosely typed JSON object (AI answers and audits are not always well typed).
type J map[string]any

func asJ(v any) J {
	switch x := v.(type) {
	case J:
		return x
	case map[string]any:
		return J(x)
	}
	return J{}
}

func (j J) M(k string) J      { return asJ(j[k]) }
func (j J) S(k string) string { return str(j[k]) }
func (j J) N(k string) float64 {
	return num(j[k])
}
func (j J) L(k string) []any {
	if l, ok := j[k].([]any); ok {
		return l
	}
	return nil
}

// Objects returns the list items that are objects.
func (j J) Objects(k string) []J {
	var out []J
	for _, x := range j.L(k) {
		if m, ok := x.(map[string]any); ok {
			out = append(out, J(m))
		}
	}
	return out
}

// Strings returns the list items as strings.
func (j J) Strings(k string) []string {
	var out []string
	for _, x := range j.L(k) {
		if s := str(x); s != "" {
			out = append(out, s)
		}
	}
	return out
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
		return ""
	case json.Number:
		return x.String()
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return fmt.Sprint(v)
}

var nonNum = regexp.MustCompile(`[^\d.]`)

// num mirrors v1 (float)preg_replace('/[^\d.]/',”,$v): "1,900" → 1900.
func num(v any) float64 {
	if f, ok := v.(float64); ok {
		return math.Abs(f)
	}
	s := nonNum.ReplaceAllString(str(v), "")
	if i := strings.IndexByte(s, '.'); i >= 0 {
		if j := strings.IndexByte(s[i+1:], '.'); j >= 0 {
			s = s[:i+1+j]
		}
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// trunc cuts to max characters with an ellipsis (v1 mb_strimwidth(…,'…')).
func trunc(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:max(0, n-1)]) + "…"
}

var glyphRepl = strings.NewReplacer("✔", "✓", "✅", "✓", "☑", "✓", "✘", "×", "✗", "×", "❌", "×", "➡", "→", "👉", "→", "⭐", "★", "🌟", "★", "—", "–")
var emojiRe = regexp.MustCompile(`[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{2B00}-\x{2BFF}\x{FE00}-\x{FE0F}\x{200D}\x{20E3}]`)
var spacesRe = regexp.MustCompile(`[ \t]{2,}`)

// cleanGlyphs replaces emojis / symbols the report fonts cannot draw (v1 Report::glyphs).
func cleanGlyphs(s string) string {
	s = glyphRepl.Replace(s)
	s = emojiRe.ReplaceAllStringFunc(s, func(m string) string {
		switch m {
		case "✓", "★", "☆", "×", "→":
			return m
		}
		return ""
	})
	return strings.TrimSpace(spacesRe.ReplaceAllString(s, " "))
}

// t escapes text for HTML after cleaning glyphs, optionally cut to max characters.
func t(s string, n int) string { return html.EscapeString(trunc(cleanGlyphs(s), n)) }

func esc(s string) string { return html.EscapeString(s) }

// hexRGB parses #RGB / #RRGGBB.
func hexRGB(hex string) ([3]int, bool) {
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return [3]int{}, false
	}
	var c [3]int
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
		if err != nil {
			return [3]int{}, false
		}
		c[i] = int(v)
	}
	return c, true
}

// tint mixes a colour with white (f=0 → colour, f=1 → white). v1 Illus::tint.
func tint(hex string, f float64) string {
	c, ok := hexRGB(hex)
	if !ok {
		c = [3]int{11, 11, 11}
	}
	for i := range c {
		c[i] = int(math.Round(float64(c[i]) + float64(255-c[i])*f))
	}
	return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
}

// isDark: background needs a light logo / light text (invalid colours count as dark, like v1).
func isDark(hex string) bool {
	c, ok := hexRGB(hex)
	if !ok {
		return true
	}
	return .299*float64(c[0])+.587*float64(c[1])+.114*float64(c[2]) < 150
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)

// safeColor keeps only #hex colours (they are written into CSS and SVG).
func safeColor(c, def string) string {
	if colorRe.MatchString(strings.TrimSpace(c)) {
		return strings.TrimSpace(c)
	}
	return def
}

// safeURL: only http(s) links are written into the report.
func safeURL(u string) string {
	u = strings.TrimSpace(u)
	l := strings.ToLower(u)
	if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
		return u
	}
	return "#"
}

var schemeRe = regexp.MustCompile(`(?i)^https?://`)

// bareHost strips the scheme and a trailing slash (v1 preg_replace('~^https?://|/$~',”,$u)).
func bareHost(u string) string {
	return strings.TrimSuffix(schemeRe.ReplaceAllString(strings.TrimSpace(u), ""), "/")
}

// numFmt formats a whole number with digit grouping (workspace style, e.g. 12,34,567).
func numFmt(v float64) string { return shared.FormatNum(v) }

// sortedKeys returns map keys sorted for stable output.
func sortedKeys[T any](m map[string]T) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// parseReport pulls the JSON object out of an AI reply (tolerates code fences, stray text and
// trailing commas). need: a key that must be present and non-empty.
func parseReport(raw, need string) J {
	s := strings.TrimSpace(fenceRe.ReplaceAllString(raw, ""))
	a, b := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if a < 0 || b < a {
		return nil
	}
	js := s[a : b+1]
	var d map[string]any
	if err := json.Unmarshal([]byte(js), &d); err != nil {
		d = nil
		if err := json.Unmarshal([]byte(trailingComma.ReplaceAllString(js, "$1")), &d); err != nil {
			return nil
		}
	}
	if d == nil || empty(d[need]) {
		return nil
	}
	return J(d)
}

var fenceRe = regexp.MustCompile("(?m)^```(?:json)?|```$")
var trailingComma = regexp.MustCompile(`,\s*([}\]])`)

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == "" || x == "0"
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case float64:
		return x == 0
	case bool:
		return !x
	}
	return false
}
