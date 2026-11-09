package reports

// Built-in flat illustrations for reports in the brand colours (v1 Illus.php).

import (
	"regexp"
	"strings"
)

// Places: report sections an uploaded illustration can be used in (v1 Report::PLACES).
var Places = [][2]string{
	{"cover", "Cover"}, {"summary", "At a glance"}, {"journey", "Market & customers"}, {"website", "Findings – website"},
	{"maps", "Findings – Google Maps"}, {"competitors", "Competitors"}, {"growth", "Opportunity"}, {"plan", "Roadmap"},
	{"tips", "Quick wins"}, {"why", "How we help"}, {"cta", "Closing page"}, {"any", "Any section"},
}

func placeOK(k string) bool {
	for _, p := range Places {
		if p[0] == k {
			return true
		}
	}
	return false
}

func placeLabel(k string) string {
	for _, p := range Places {
		if p[0] == k {
			return p[1]
		}
	}
	return k
}

var illusTok = regexp.MustCompile(`\$(pl|pm|al|bg|p|a|g|r|o|v|w)\b`)

// illusSVG returns the built-in illustration for a section in brand colours P (primary) and A (accent).
func illusSVG(name, P, A string) string {
	P, A = safeColor(P, "#0B0B0B"), safeColor(A, "#B8860B")
	col := map[string]string{
		"p": P, "a": A, "pl": tint(P, .88), "pm": tint(P, .6), "al": tint(A, .75), "bg": tint(A, .9),
		"g": "#2EAD6B", "r": "#E8554E", "o": "#F28C28", "v": "#7B61D9", "w": "#FFFFFF",
	}
	body, ok := illusBody[name]
	if !ok {
		body = "<circle cx='200' cy='130' r='100' fill='$pl'/>"
	}
	body = illusTok.ReplaceAllStringFunc(body, func(m string) string { return col[m[1:]] })
	return "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 400 260' width='400' height='260'>" + body + "</svg>"
}

// illusURI is the illustration as a data URI.
func illusURI(name, P, A string) string { return svgURI(illusSVG(name, P, A)) }

// illusForHeading picks an illustration that fits a section heading (v1 Illus::forHeading).
func illusForHeading(h string) string {
	h = strings.ToLower(h)
	for _, p := range [][2]string{{"summary", "summary"}, {"find you", "journey"}, {"journey", "journey"}, {"website", "website"}, {"blueprint", "website"}, {"maps", "maps"}, {"local", "maps"}, {"competitor", "competitors"}, {"revenue", "growth"}, {"opportunity", "growth"}, {"number", "growth"}, {"plan", "plan"}, {"quick win", "tips"}, {"tips", "tips"}, {"why", "why"}, {"next step", "cta"}} {
		if strings.Contains(h, p[0]) {
			return p[1]
		}
	}
	return ""
}
