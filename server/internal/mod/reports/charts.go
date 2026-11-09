package reports

// Vector charts (SVG data URIs) for the report – port of v1 Charts.php, drawn at 2x for crisp output.

import (
	"encoding/base64"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
)

const chartFont = "font-family='Inter'"

// chartPal is the categorical palette used across the report (v1 Charts::PAL).
var chartPal = []string{"#3B82F6", "#10B981", "#F59E0B", "#8B5CF6", "#14B8A6", "#EF4444", "#EC4899", "#6366F1"}

func scoreCol(s int) string {
	switch {
	case s >= 75:
		return "#10B981"
	case s >= 50:
		return "#F59E0B"
	}
	return "#EF4444"
}

func scoreSoft(s int) string {
	switch {
	case s >= 75:
		return "#D1FAE5"
	case s >= 50:
		return "#FEF3C7"
	}
	return "#FEE2E2"
}

// svgURI turns an SVG document into a base64 data URI.
func svgURI(svg string) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func svgDoc(w, h float64, body string) string {
	return svgURI(fmt.Sprintf("<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 %s %s' width='%s' height='%s'>%s</svg>", fn(w), fn(h), fn(w), fn(h), body))
}

// fn formats a coordinate (max 2 decimals, no trailing zeros).
func fn(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }

func xmlEsc(s string) string { return html.EscapeString(s) }

func arcPath(cx, cy, r, f float64) string {
	f = math.Max(0, math.Min(.9999, f))
	pt := func(t float64) string {
		a := t*2*math.Pi - math.Pi/2
		return fn(cx+r*math.Cos(a)) + " " + fn(cy+r*math.Sin(a))
	}
	s := fmt.Sprintf("M%s %s", fn(cx), fn(cy-r))
	if f > .5 {
		return s + fmt.Sprintf(" A%s %s 0 0 1 %s A%s %s 0 0 1 %s", fn(r), fn(r), pt(.5), fn(r), fn(r), pt(f))
	}
	return s + fmt.Sprintf(" A%s %s 0 0 1 %s", fn(r), fn(r), pt(f))
}

// chartGauge: ring gauge with the score in the centre.
func chartGauge(score int, label string, size float64) string {
	s := max(0, min(100, score))
	c := scoreCol(s)
	cx, cy := size/2, size/2
	sw := size * .11
	r := cx - sw/2 - 2
	arc := ""
	if s >= 100 {
		arc = fmt.Sprintf("<circle cx='%s' cy='%s' r='%s' fill='none' stroke='%s' stroke-width='%s'/>", fn(cx), fn(cy), fn(r), c, fn(sw))
	} else if s > 0 {
		arc = fmt.Sprintf("<path d='%s' fill='none' stroke='%s' stroke-width='%s' stroke-linecap='round'/>", arcPath(cx, cy, r, float64(s)/100), c, fn(sw))
	}
	b := fmt.Sprintf("<circle cx='%s' cy='%s' r='%s' fill='none' stroke='#EEF1F6' stroke-width='%s'/>%s<text x='%s' y='%s' font-size='%s' font-weight='bold' text-anchor='middle' fill='#0F172A' %s>%d</text>",
		fn(cx), fn(cy), fn(r), fn(sw), arc, fn(cx), fn(math.Round(cy+size*.09)), fn(math.Round(size*.27)), chartFont, s)
	if label != "" {
		b += fmt.Sprintf("<text x='%s' y='%s' font-size='%s' text-anchor='middle' fill='#64748B' %s>%s</text>", fn(cx), fn(math.Round(cy+size*.22)), fn(math.Round(size*.075)), chartFont, xmlEsc(label))
	}
	return svgDoc(size, size, b)
}

type kv struct {
	K string
	V float64
}

// chartBars: horizontal score bars with track and value label.
func chartBars(items []kv, w float64) string {
	h := float64(len(items))*44 + 6
	var o strings.Builder
	y := 4.0
	lw := 170.0
	bw := w - lw - 56
	for _, it := range items {
		v := int(it.V)
		c := scoreCol(v)
		fw := math.Max(14, math.Round(bw*float64(v)/100))
		fmt.Fprintf(&o, "<text x='0' y='%s' font-size='15' fill='#334155' %s>%s</text><rect x='%s' y='%s' width='%s' height='18' rx='9' fill='#EEF1F6'/><rect x='%s' y='%s' width='%s' height='18' rx='9' fill='%s'/><text x='%s' y='%s' font-size='16' font-weight='bold' text-anchor='end' fill='#0F172A' %s>%d</text>",
			fn(y+20), chartFont, xmlEsc(it.K), fn(lw), fn(y+6), fn(bw), fn(lw), fn(y+6), fn(fw), c, fn(w-4), fn(y+21), chartFont, v)
		y += 44
	}
	return svgDoc(w, h, o.String())
}

type cmpRow struct {
	Label string
	V     float64
	Me    bool
}

// chartCompare compares a value across businesses, highlighting "you".
func chartCompare(rows []cmpRow, unit string, w float64, hl string) string {
	mx := 1.0
	for _, r := range rows {
		mx = math.Max(mx, r.V)
	}
	h := float64(len(rows))*42 + 6
	var o strings.Builder
	y := 4.0
	lw := 200.0
	bw := w - lw - 70
	for _, r := range rows {
		fw := math.Max(6, math.Round(bw*r.V/mx))
		c, tc, fwt := "#CBD5E1", "#475569", "normal"
		if r.Me {
			c, tc, fwt = hl, "#0F172A", "bold"
		}
		fmt.Fprintf(&o, "<text x='0' y='%s' font-size='14' fill='%s' font-weight='%s' %s>%s</text><rect x='%s' y='%s' width='%s' height='20' rx='6' fill='%s'/><text x='%s' y='%s' font-size='14' font-weight='bold' fill='#0F172A' %s>%s</text>",
			fn(y+21), tc, fwt, chartFont, xmlEsc(trunc(r.Label, 26)), fn(lw), fn(y+7), fn(fw), c, fn(lw+fw+8), fn(y+22), chartFont, xmlEsc(fn(r.V)+unit))
		y += 42
	}
	return svgDoc(w, h, o.String())
}

type colItem struct {
	Label, Color, Text string
	V                  float64
}

// chartColumns: grouped "now vs potential" columns.
func chartColumns(cols []colItem, w, h float64) string {
	mx := 1.0
	for _, c := range cols {
		mx = math.Max(mx, c.V)
	}
	n := float64(max(1, len(cols)))
	slot := (w - 20) / n
	bw := math.Min(120, slot*.55)
	base := h - 46
	var o strings.Builder
	fmt.Fprintf(&o, "<path d='M10 %s H%s' stroke='#CBD5E1' stroke-width='2'/>", fn(base), fn(w-10))
	for g := 1; g <= 3; g++ {
		gy := math.Round(base - (base-30)*float64(g)/3)
		fmt.Fprintf(&o, "<path d='M10 %s H%s' stroke='#EEF1F6' stroke-width='1.5'/>", fn(gy), fn(w-10))
	}
	for i, c := range cols {
		x := 10 + float64(i)*slot + (slot-bw)/2
		bh := math.Max(4, math.Round((base-40)*c.V/mx))
		fmt.Fprintf(&o, "<rect x='%s' y='%s' width='%s' height='%s' rx='10' fill='%s'/><text x='%s' y='%s' font-size='18' font-weight='bold' text-anchor='middle' fill='#0F172A' %s>%s</text><text x='%s' y='%s' font-size='14' text-anchor='middle' fill='#475569' %s>%s</text>",
			fn(x), fn(base-bh), fn(bw), fn(bh), c.Color, fn(x+bw/2), fn(base-bh-10), chartFont, xmlEsc(c.Text), fn(x+bw/2), fn(base+26), chartFont, xmlEsc(c.Label))
	}
	return svgDoc(w, h, o.String())
}

type funnelStep struct{ Label, Value, Color string }

// chartFunnel: customer funnel searches → views → actions.
func chartFunnel(steps []funnelStep, w float64) string {
	h := float64(len(steps)) * 70
	var o strings.Builder
	for i, s := range steps {
		top := w - float64(i)*(w*.18)
		bot := w - float64(i+1)*(w*.18)
		y := float64(i) * 70
		x1, x2 := (w-top)/2, (w-bot)/2
		fmt.Fprintf(&o, "<path d='M%s %s H%s L%s %s H%s Z' fill='%s'/><text x='%s' y='%s' font-size='19' font-weight='bold' text-anchor='middle' fill='#FFFFFF' %s>%s</text><text x='%s' y='%s' font-size='12.5' text-anchor='middle' fill='#FFFFFF' %s>%s</text>",
			fn(x1), fn(y), fn(x1+top), fn(x2+bot), fn(y+62), fn(x2), s.Color, fn(w/2), fn(y+30), chartFont, xmlEsc(s.Value), fn(w/2), fn(y+50), chartFont, xmlEsc(s.Label))
	}
	return svgDoc(w, h, o.String())
}

// chartDonut: pass / warn / fail donut.
func chartDonut(pass, warn, fail int, size float64) string {
	t := max(1, pass+warn+fail)
	cx, cy := size/2, size/2
	r, sw := size*.38, size*.16
	var o strings.Builder
	fmt.Fprintf(&o, "<circle cx='%s' cy='%s' r='%s' fill='none' stroke='#EEF1F6' stroke-width='%s'/>", fn(cx), fn(cy), fn(r), fn(sw))
	acc := 0.0
	for _, p := range []struct {
		v int
		c string
	}{{pass, "#10B981"}, {warn, "#F59E0B"}, {fail, "#EF4444"}} {
		if p.v == 0 {
			continue
		}
		ln := 2 * math.Pi * r
		seg := ln * float64(p.v) / float64(t)
		fmt.Fprintf(&o, "<circle cx='%s' cy='%s' r='%s' fill='none' stroke='%s' stroke-width='%s' stroke-dasharray='%s %s' stroke-dashoffset='%s' transform='rotate(-90 %s %s)'/>",
			fn(cx), fn(cy), fn(r), p.c, fn(sw), fn(seg), fn(ln), fn(-acc), fn(cx), fn(cy))
		acc += seg
	}
	fmt.Fprintf(&o, "<text x='%s' y='%s' font-size='%s' font-weight='bold' text-anchor='middle' fill='#0F172A' %s>%d</text><text x='%s' y='%s' font-size='%s' text-anchor='middle' fill='#64748B' %s>checks</text>",
		fn(cx), fn(cy+6), fn(math.Round(size*.17)), chartFont, t, fn(cx), fn(cy+math.Round(size*.13)), fn(math.Round(size*.07)), chartFont)
	return svgDoc(size, size, o.String())
}

// chartIcon: small pass / warn / fail status icon.
func chartIcon(kind string, size float64) string {
	m := map[string][2]string{
		"pass": {"#10B981", "<path d='M7 12.5l3.2 3.2L17 9' fill='none' stroke='#fff' stroke-width='2.6' stroke-linecap='round' stroke-linejoin='round'/>"},
		"warn": {"#F59E0B", "<path d='M12 7v6' stroke='#fff' stroke-width='2.6' stroke-linecap='round'/><circle cx='12' cy='16.6' r='1.5' fill='#fff'/>"},
		"fail": {"#EF4444", "<path d='M8.5 8.5l7 7M15.5 8.5l-7 7' stroke='#fff' stroke-width='2.6' stroke-linecap='round'/>"},
	}
	p, ok := m[kind]
	if !ok {
		p = m["warn"]
	}
	return svgDoc(size, size, fmt.Sprintf("<g transform='scale(%s)'><circle cx='12' cy='12' r='12' fill='%s'/>%s</g>", fn(size/24), p[0], p[1]))
}

var glyphs = map[string]string{
	"maps":       "<path d='M20 10c0 6-8 12-8 12s-8-6-8-12a8 8 0 0 1 16 0Z'/><circle cx='12' cy='10' r='3'/>",
	"website":    "<rect x='3' y='4' width='18' height='16' rx='2'/><path d='M3 9h18'/>",
	"reviews":    "<path d='M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01z'/>",
	"content":    "<path d='M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z'/><path d='M14 2v5h5M8 13h8M8 17h6'/>",
	"conversion": "<circle cx='12' cy='12' r='9'/><circle cx='12' cy='12' r='5'/><circle cx='12' cy='12' r='1.5'/>",
	"speed":      "<path d='M13 2 3 14h9l-1 8 10-12h-9l1-8z'/>",
	"trust":      "<path d='M20 13c0 5-3.5 7.5-8 9-4.5-1.5-8-4-8-9V6l8-3 8 3z'/>",
	"social":     "<circle cx='18' cy='5' r='3'/><circle cx='6' cy='12' r='3'/><circle cx='18' cy='19' r='3'/><path d='m8.6 13.5 6.8 4M15.4 6.5l-6.8 4'/>",
	"growth":     "<path d='M3 17l6-6 4 4 8-8M15 7h6v6'/>",
	"calendar":   "<rect x='3' y='4' width='18' height='18' rx='2'/><path d='M16 2v4M8 2v4M3 10h18'/>",
	"idea":       "<path d='M9 18h6M10 22h4M12 2a7 7 0 0 0-4 12.7V16h8v-1.3A7 7 0 0 0 12 2z'/>",
	"phone":      "<path d='M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 4.2 2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1.9.4 1.9.7 2.8a2 2 0 0 1-.5 2.1L8.1 9.9a16 16 0 0 0 6 6l1.3-1.3a2 2 0 0 1 2.1-.4c.9.3 1.9.6 2.8.7a2 2 0 0 1 1.7 2z'/>",
	"users":      "<circle cx='9' cy='7' r='4'/><path d='M3 21v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2M16 3.1a4 4 0 0 1 0 7.8M21 21v-2a4 4 0 0 0-3-3.9'/>",
}

// chartGlyph: line icon (area of a finding) in one colour.
func chartGlyph(name, color string, size float64) string {
	p, ok := glyphs[name]
	if !ok {
		p = "<circle cx='12' cy='12' r='9'/>"
	}
	return svgDoc(size, size, fmt.Sprintf("<g transform='scale(%s)' fill='none' stroke='%s' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'>%s</g>", fn(size/24), color, p))
}

type issueRow struct {
	Label      string
	Fail, Warn int
}

// chartIssues: stacked problem bars per area.
func chartIssues(rows []issueRow, w float64) string {
	mx := 1
	for _, r := range rows {
		mx = max(mx, r.Fail+r.Warn)
	}
	h := float64(len(rows))*46 + 6
	var o strings.Builder
	y := 4.0
	lw := 190.0
	bw := w - lw - 60
	for _, r := range rows {
		fw, ww := 0.0, 0.0
		if r.Fail > 0 {
			fw = math.Max(10, math.Round(bw*float64(r.Fail)/float64(mx)))
		}
		if r.Warn > 0 {
			ww = math.Max(10, math.Round(bw*float64(r.Warn)/float64(mx)))
		}
		fmt.Fprintf(&o, "<text x='0' y='%s' font-size='15' fill='#334155' %s>%s</text><rect x='%s' y='%s' width='%s' height='22' rx='7' fill='#F1F5F9'/>", fn(y+22), chartFont, xmlEsc(trunc(r.Label, 24)), fn(lw), fn(y+7), fn(bw))
		if fw > 0 {
			fmt.Fprintf(&o, "<rect x='%s' y='%s' width='%s' height='22' rx='7' fill='#EF4444'/>", fn(lw), fn(y+7), fn(fw))
		}
		if ww > 0 {
			gap := 0.0
			if fw > 0 {
				gap = 3
			}
			fmt.Fprintf(&o, "<rect x='%s' y='%s' width='%s' height='22' rx='7' fill='#F59E0B'/>", fn(lw+fw+gap), fn(y+7), fn(ww))
		}
		fmt.Fprintf(&o, "<text x='%s' y='%s' font-size='16' font-weight='bold' text-anchor='end' fill='#0F172A' %s>%d</text>", fn(w-4), fn(y+24), chartFont, r.Fail+r.Warn)
		y += 46
	}
	return svgDoc(w, h, o.String())
}
