package crm

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Core lead fields (v1 CRM::CORE).
var Core = []string{"name", "contact_name", "email", "phone", "alt_phone", "website", "address", "city", "category", "service", "deal_value", "message", "tags", "follow_up_at"}

var coreSet = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range Core {
		m[k] = true
	}
	return m
}()

// IsCore reports a built-in lead field.
func IsCore(k string) bool { return coreSet[k] }

// alias: incoming key → Setu field, in v1 order (first match wins).
var alias = []struct {
	f    string
	alts []string
}{
	{"name", []string{"company", "company_name", "business", "business_name", "organisation", "organization", "firm", "shop_name"}},
	{"contact_name", []string{"name", "full_name", "fullname", "your_name", "first_name", "contact", "contact_name", "customer_name", "person"}},
	{"email", []string{"email", "email_address", "e_mail", "your_email", "mail", "work_email"}},
	{"phone", []string{"phone", "phone_number", "mobile", "mobile_number", "contact_number", "whatsapp", "whatsapp_number", "tel", "telephone", "your_phone", "number", "work_phone"}},
	{"alt_phone", []string{"alt_phone", "alternate_phone", "phone_2", "landline"}},
	{"city", []string{"city", "location", "town"}},
	{"address", []string{"address", "street_address", "full_address"}},
	{"website", []string{"website", "url", "site", "web", "website_url"}},
	{"service", []string{"service", "services", "interested_in", "product", "requirement_type", "enquiry_for", "subject", "select_service"}},
	{"message", []string{"message", "your_message", "comments", "comment", "query", "enquiry", "details", "description", "notes", "requirement"}},
	{"deal_value", []string{"budget", "deal_value", "amount", "value"}},
}

// loose: substring match for labels like "Your mobile number" (v1 order).
var loose = [][2]string{{"email", "email"}, {"mail", "email"}, {"phone", "phone"}, {"mobile", "phone"}, {"whatsapp", "phone"}, {"contact_no", "phone"}, {"company", "name"}, {"business", "name"}, {"budget", "deal_value"}, {"service", "service"}, {"city", "city"}, {"address", "address"}, {"website", "website"}, {"message", "message"}, {"requirement", "message"}, {"enquiry", "message"}, {"name", "contact_name"}}

// Form-tool metadata that should not become lead fields.
var metaKeys = map[string]bool{"page_url": true, "user_agent": true, "remote_ip": true, "powered_by": true, "referrer": true, "referer": true, "form_id": true, "form_name": true, "queried_id": true, "g_recaptcha_response": true, "_csrf": true, "k": true, "redirect": true, "_hp": true, "submit": true, "action": true, "nonce": true, "wpnonce": true, "hp": true, "csrf": true, "wp_http_referer": true}

var nonAlnum = regexp.MustCompile(`(?i)[^a-z0-9]+`)

// Key normalises an incoming field name ("Your Phone" → "your_phone").
func Key(k string) string {
	return strings.Trim(strings.ToLower(nonAlnum.ReplaceAllString(strings.TrimSpace(k), "_")), "_")
}

// Auto returns the Setu field an incoming key fills automatically: a core field, "cf:<key>" for a
// custom field, or "" (kept in the enquiry notes). fieldKeys are the workspace's custom field keys.
func Auto(nk string, fieldKeys []string) string {
	for _, a := range alias {
		for _, x := range a.alts {
			if x == nk {
				return a.f
			}
		}
	}
	for _, f := range fieldKeys {
		if f == nk {
			return "cf:" + f
		}
	}
	if nk == "last_name" {
		return "contact_name"
	}
	for _, l := range loose {
		if strings.Contains(nk, l[0]) {
			return l[1]
		}
	}
	return ""
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case bool:
		if x {
			return "1", true
		}
		return "", true
	case interface{ String() string }: // json.Number
		return x.String(), true
	}
	return "", false
}

// sortedKeys gives a stable order (Go maps have none): alphabetical, which keeps first_name before last_name.
func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// kv is an ordered key/value list.
type kv struct{ k, v string }

// flatten handles payloads from Elementor (simple and "Advanced data"), nested JSON etc.
func flatten(in map[string]any) (out []kv, labels, meta map[string]string) {
	labels, meta = map[string]string{}, map[string]string{}
	src := in
	var lead []kv
	if fs, ok := in["fields"].(map[string]any); ok {
		var f []kv
		for _, k := range sortedKeys(fs) {
			x, ok := fs[k].(map[string]any)
			if !ok {
				continue
			}
			val, has := x["value"]
			if !has {
				continue
			}
			idv := k
			if s, ok := scalar(x["id"]); ok && s != "" {
				idv = s
			}
			id := Key(idv)
			sv, _ := scalar(val)
			if l, ok := x["value"].([]any); ok {
				sv = joinScalars(l)
			}
			f = append(f, kv{id, sv})
			if t, ok := scalar(x["title"]); ok && t != "" {
				labels[id] = t
			} else {
				labels[id] = id
			}
		}
		if len(f) > 0 {
			if m, ok := in["meta"].(map[string]any); ok {
				for _, k := range sortedKeys(m) {
					if mm, ok := m[k].(map[string]any); ok {
						s, _ := scalar(mm["value"])
						meta[Key(k)] = s
					} else {
						s, _ := scalar(m[k])
						meta[Key(k)] = s
					}
				}
			}
			if fm, ok := in["form"].(map[string]any); ok {
				if s, ok := scalar(fm["name"]); ok {
					meta["form_name"] = s
				}
			}
			lead = f
			src = map[string]any{}
			for k, v := range in {
				if k != "fields" && k != "meta" && k != "form" {
					src[k] = v
				}
			}
		}
	}
	seen := map[string]int{}
	put := func(k, v, label string, overwriteLabel bool) {
		if i, ok := seen[k]; ok {
			out[i].v = v
		} else {
			seen[k] = len(out)
			out = append(out, kv{k, v})
		}
		if _, ok := labels[k]; !ok || overwriteLabel {
			labels[k] = label
		}
	}
	// Fields from the Elementor "fields" block win over top-level keys of the same name
	for _, x := range lead {
		put(x.k, x.v, labels[x.k], false)
	}
	fromFields := map[string]bool{}
	for _, x := range lead {
		fromFields[x.k] = true
	}
	for _, k := range sortedKeys(src) {
		v := src[k]
		nk := Key(k)
		if nk == "" || fromFields[nk] {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			if len(x) == 0 {
				continue
			}
			px := nk + "_"
			switch nk {
			case "form_fields", "fields", "data", "lead", "contact", "input", "form":
				px = ""
			}
			for _, k2 := range sortedKeys(x) {
				if s, ok := scalar(x[k2]); ok {
					lb := k2
					if px != "" {
						lb = k + " › " + k2
					}
					put(px+Key(k2), s, lb, true)
				}
			}
			continue
		case []any:
			put(nk, joinScalars(x), k, false)
		case nil:
			continue
		default:
			s, _ := scalar(x)
			put(nk, s, k, false)
		}
	}
	_, pw := seen["powered_by"]
	kept := out[:0]
	for _, x := range out {
		if metaKeys[x.k] || (pw && (x.k == "date" || x.k == "time")) {
			meta[x.k] = x.v
			continue
		}
		kept = append(kept, x)
	}
	return kept, labels, meta
}

func joinScalars(l []any) string {
	var p []string
	for _, y := range l {
		if s, ok := scalar(y); ok {
			p = append(p, s)
		}
	}
	return strings.Join(p, ", ")
}

// Flatten exposes v1 CRM::flatten: incoming fields (key → value), their labels and form metadata
// (page URL, form name, …).
func Flatten(in map[string]any) (fields, labels, meta map[string]string) {
	out, labels, meta := flatten(in)
	fields = make(map[string]string, len(out))
	for _, x := range out {
		fields[x.k] = x.v
	}
	return fields, labels, meta
}

// normalizeOrdered is v1 CRM::normalize; rest keeps the incoming order.
func normalizeOrdered(in map[string]any, mapping map[string]string, fieldKeys []string) (core, custom map[string]string, rest []kv, labels map[string]string) {
	flat, labels, _ := flatten(in)
	core, custom = map[string]string{}, map[string]string{}
	for _, x := range flat {
		nk, v := x.k, strings.TrimSpace(x.v)
		if v == "" {
			continue
		}
		t := mapping[nk]
		if t == "" {
			t = Auto(nk, fieldKeys)
		}
		if t == "" {
			if lb, ok := labels[nk]; ok {
				t = Auto(Key(lb), fieldKeys)
			}
		}
		if t == "ignore" {
			continue
		}
		if strings.HasPrefix(t, "cf:") {
			custom[t[3:]] = v
			continue
		}
		if t == "message" {
			if _, ok := core["message"]; ok {
				rest = append(rest, kv{nk, v})
				continue
			}
		}
		if t != "" && coreSet[t] {
			cur, has := core[t]
			switch {
			case has && (t == "contact_name" || t == "name" || t == "address"):
				core[t] = cur + " " + v
			case !has:
				core[t] = v
			default:
				rest = append(rest, kv{nk, v})
			}
			continue
		}
		rest = append(rest, kv{nk, v})
	}
	return
}

// Normalize maps an incoming payload (form post, webhook, CSV row) to Setu fields (v1 flatten +
// normalize). mapping = incoming key → setu field | "cf:<key>" | "message" | "ignore" overrides the
// automatic detection. Returns core lead fields, custom field values, unmatched fields (kept in the
// enquiry notes) and the label of every incoming key. Custom fields are only matched through mapping;
// use NormalizeFields to also match the workspace's custom field keys automatically.
func Normalize(in map[string]any, mapping map[string]string) (core map[string]string, custom map[string]string, rest map[string]string, labels map[string]string) {
	return NormalizeFields(in, mapping, nil)
}

// NormalizeFields is Normalize with the workspace's custom field keys (see Fields) for auto-matching.
func NormalizeFields(in map[string]any, mapping map[string]string, fieldKeys []string) (core map[string]string, custom map[string]string, rest map[string]string, labels map[string]string) {
	core, custom, r, labels := normalizeOrdered(in, mapping, fieldKeys)
	rest = make(map[string]string, len(r))
	for _, x := range r {
		rest[x.k] = x.v
	}
	return core, custom, rest, labels
}

// RestText formats unmatched fields for the enquiry message ("key: value" lines, sorted).
func RestText(rest map[string]string) string {
	var b strings.Builder
	for _, k := range sortedKeys(rest) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k + ": " + rest[k])
	}
	return b.String()
}
