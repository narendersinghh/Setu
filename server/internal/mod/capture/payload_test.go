package capture

import "testing"

func TestDecodeQueryPHP(t *testing.T) {
	o := decodeQuery("fields[name][id]=name&fields[name][value]=Sita&tags[]=a&tags[]=b&x=1")
	f := flatten(o)
	if f.vals["name"] != "Sita" || f.vals["tags"] != "a, b" || f.vals["x"] != "1" {
		t.Fatalf("got %#v", f.vals)
	}
	if f.keys[0] != "name" {
		t.Fatalf("order %v", f.keys)
	}
}

func TestFlattenNestedAndMeta(t *testing.T) {
	o, ok := decodeJSON([]byte(`{"Your Name":"A","contact":{"phone":"1"},"extra":{"city":"Pune"},"page_url":"u"}`))
	if !ok {
		t.Fatal("decode")
	}
	f := flatten(o)
	if f.vals["your_name"] != "A" || f.vals["phone"] != "1" || f.vals["extra_city"] != "Pune" || f.meta["page_url"] != "u" {
		t.Fatalf("got %#v %#v", f.vals, f.meta)
	}
	if f.labels["extra_city"] != "extra › city" {
		t.Fatalf("label %q", f.labels["extra_city"])
	}
}
