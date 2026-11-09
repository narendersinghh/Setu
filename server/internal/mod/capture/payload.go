package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// omap is an ordered JSON-like object: form fields keep the order the visitor's form sent them in
// (used for the field list in Settings and the order of extra answers in the enquiry notes).
type omap []entry

type entry struct {
	K string
	V any // string | json.Number | bool | nil | omap | []any
}

func (o omap) get(k string) (any, bool) {
	for _, e := range o {
		if e.K == k {
			return e.V, true
		}
	}
	return nil, false
}

func (o omap) str(k string) string {
	v, _ := o.get(k)
	return scalar(v)
}

func (o *omap) set(k string, v any) {
	for i, e := range *o {
		if e.K == k {
			(*o)[i].V = v
			return
		}
	}
	*o = append(*o, entry{k, v})
}

func (o omap) without(keys ...string) omap {
	out := omap{}
	for _, e := range o {
		skip := false
		for _, k := range keys {
			if e.K == k {
				skip = true
			}
		}
		if !skip {
			out = append(out, e)
		}
	}
	return out
}

// isList: keys 0..n-1 (PHP sequential array).
func (o omap) isList() bool {
	for i, e := range o {
		if e.K != strconv.Itoa(i) {
			return false
		}
	}
	return true
}

func (o omap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.K)
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(e.V)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// plain converts to map[string]any / []any (input for crm.Normalize).
func plain(v any) any {
	switch x := v.(type) {
	case omap:
		m := make(map[string]any, len(x))
		for _, e := range x {
			m[e.K] = plain(e.V)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = plain(y)
		}
		return out
	case json.Number:
		return x.String()
	}
	return v
}

func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "1"
		}
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func isScalar(v any) bool {
	switch v.(type) {
	case omap, []any:
		return false
	}
	return true
}

// ---------------------------------------------------------------- decoding

// decodeJSON parses a JSON body keeping object key order. Arrays at the top level become
// objects with index keys (like PHP).
func decodeJSON(b []byte) (omap, bool) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, err := jsonValue(d)
	if err != nil {
		return nil, false
	}
	switch x := v.(type) {
	case omap:
		return x, true
	case []any:
		o := omap{}
		for i, y := range x {
			o = append(o, entry{strconv.Itoa(i), y})
		}
		return o, true
	}
	return nil, false
}

func jsonValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '{':
			o := omap{}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := jsonValue(d)
				if err != nil {
					return nil, err
				}
				o.set(k, v)
			}
			_, err := d.Token()
			return o, err
		case '[':
			l := []any{}
			for d.More() {
				v, err := jsonValue(d)
				if err != nil {
					return nil, err
				}
				l = append(l, v)
			}
			_, err := d.Token()
			return l, err
		}
	}
	return t, nil
}

// decodeQuery parses an urlencoded string in order, with PHP-style keys (fields[name][value]=…),
// which form plugins such as Elementor send.
func decodeQuery(s string) omap {
	o := omap{}
	for _, p := range strings.Split(s, "&") {
		if p == "" {
			continue
		}
		k, v, _ := strings.Cut(p, "=")
		k, err1 := url.QueryUnescape(k)
		v, err2 := url.QueryUnescape(v)
		if err1 != nil || err2 != nil || k == "" {
			continue
		}
		setPath(&o, k, v)
	}
	return o
}

// decodeMultipart parses multipart/form-data (e.g. navigator.sendBeacon with FormData); files are skipped.
func decodeMultipart(body []byte, boundary string) omap {
	o := omap{}
	r := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := r.NextPart()
		if err != nil {
			break
		}
		if p.FileName() != "" || p.FormName() == "" {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(p, 64<<10))
		setPath(&o, p.FormName(), string(b))
	}
	return o
}

var bracket = regexp.MustCompile(`\[([^\]]*)\]`)

// setPath stores name[a][b] / name[] like PHP's $_POST.
func setPath(o *omap, name, val string) {
	base, rest := name, ""
	if i := strings.IndexByte(name, '['); i > 0 && strings.HasSuffix(name, "]") {
		base, rest = name[:i], name[i:]
	}
	path := []string{base}
	for _, m := range bracket.FindAllStringSubmatch(rest, 8) {
		path = append(path, m[1])
	}
	*o = setRec(*o, path, val)
}

func setRec(o omap, path []string, val string) omap {
	k := path[0]
	if k == "" { // name[] → next index
		k = strconv.Itoa(len(o))
	}
	if len(path) == 1 {
		o.set(k, val)
		return o
	}
	child, _ := o.get(k)
	co, _ := child.(omap)
	o.set(k, setRec(co, path[1:], val))
	return o
}

// ---------------------------------------------------------------- flattening (port of v1 CRM::flatten)

var nonKey = regexp.MustCompile(`[^a-z0-9]+`)

// key normalises a field name: "Your Mobile No." → "your_mobile_no".
func key(k string) string {
	return strings.Trim(nonKey.ReplaceAllString(strings.ToLower(strings.TrimSpace(k)), "_"), "_")
}

// metaKeys are form-tool metadata that never become lead fields (v1 CRM::META).
var metaKeys = map[string]bool{"page_url": true, "user_agent": true, "remote_ip": true, "powered_by": true, "referrer": true, "referer": true, "form_id": true, "form_name": true, "queried_id": true, "g_recaptcha_response": true, "_csrf": true, "k": true, "redirect": true, "_hp": true, "submit": true, "action": true, "nonce": true, "wpnonce": true, "hp": true, "csrf": true, "wp_http_referer": true}

var groupKeys = map[string]bool{"form_fields": true, "fields": true, "data": true, "lead": true, "contact": true, "input": true, "form": true}

type flat struct {
	keys   []string          // field keys in order
	vals   map[string]string // key → value
	labels map[string]string // key → label as sent
	meta   map[string]string
}

// flatten handles Elementor (simple + "Advanced data"), nested JSON and PHP-style form arrays.
func flatten(in omap) flat {
	f := flat{vals: map[string]string{}, labels: map[string]string{}, meta: map[string]string{}}
	if fv, ok := in.get("fields"); ok {
		var items []entry
		switch x := fv.(type) {
		case omap:
			items = x
		case []any:
			for i, y := range x {
				items = append(items, entry{strconv.Itoa(i), y})
			}
		}
		adv := omap{}
		for _, it := range items {
			xo, ok := it.V.(omap)
			if !ok {
				continue
			}
			v, has := xo.get("value")
			if !has {
				continue
			}
			idv := it.K
			if s := xo.str("id"); s != "" {
				idv = s
			}
			id := key(idv)
			adv.set(id, v)
			if t := xo.str("title"); t != "" {
				f.labels[id] = t
			} else {
				f.labels[id] = id
			}
		}
		if len(adv) > 0 || len(items) > 0 {
			if m, ok := in.get("meta"); ok {
				if mo, ok := m.(omap); ok {
					for _, e := range mo {
						if xo, ok := e.V.(omap); ok {
							f.meta[key(e.K)] = xo.str("value")
						} else {
							f.meta[key(e.K)] = scalar(e.V)
						}
					}
				}
			}
			if fm, ok := in.get("form"); ok {
				if fo, ok := fm.(omap); ok && fo.str("name") != "" {
					f.meta["form_name"] = fo.str("name")
				}
			}
			for _, e := range in.without("fields", "meta", "form") {
				if _, dup := adv.get(e.K); !dup {
					adv = append(adv, e)
				}
			}
			in = adv
		}
	}
	add := func(k, v, label string) {
		if _, ok := f.vals[k]; !ok {
			f.keys = append(f.keys, k)
		}
		f.vals[k] = v
		if _, ok := f.labels[k]; !ok {
			f.labels[k] = label
		}
	}
	for _, e := range in {
		nk := key(e.K)
		if nk == "" {
			continue
		}
		switch x := e.V.(type) {
		case omap:
			if len(x) > 0 && !x.isList() {
				px := nk + "_"
				if groupKeys[nk] {
					px = ""
				}
				for _, e2 := range x {
					if !isScalar(e2.V) {
						continue
					}
					k2 := px + key(e2.K)
					lab := e2.K
					if px != "" {
						lab = e.K + " › " + e2.K
					}
					f.labels[k2] = lab
					add(k2, scalar(e2.V), lab)
				}
				continue
			}
			var parts []string
			for _, e2 := range x {
				if isScalar(e2.V) {
					parts = append(parts, scalar(e2.V))
				}
			}
			add(nk, strings.Join(parts, ", "), e.K)
		case []any:
			var parts []string
			for _, y := range x {
				if isScalar(y) {
					parts = append(parts, scalar(y))
				}
			}
			add(nk, strings.Join(parts, ", "), e.K)
		default:
			add(nk, scalar(x), e.K)
		}
	}
	_, pw := f.vals["powered_by"]
	keep := f.keys[:0:0]
	for _, k := range f.keys {
		if metaKeys[k] || (pw && (k == "date" || k == "time")) {
			f.meta[k] = f.vals[k]
			delete(f.vals, k)
			continue
		}
		keep = append(keep, k)
	}
	f.keys = keep
	return f
}
