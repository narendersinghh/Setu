package crm

import (
	"encoding/json"
	"testing"
)

func TestNormalizeElementorAdvanced(t *testing.T) {
	var in map[string]any
	json.Unmarshal([]byte(`{"form":{"id":"a1","name":"Contact page"},"fields":{"name":{"id":"name","title":"Your Name","value":"Ravi"},"field_2":{"id":"field_2","title":"Mobile number","value":"98765 43210"},"email":{"id":"email","title":"Email","value":"r@x.in"},"msg":{"id":"msg","title":"Message","value":"Need SEO"},"field_9":{"id":"field_9","title":"Branches","value":"3"}},"meta":{"page_url":{"title":"Page URL","value":"https://x.in/contact"}}}`), &in)
	core, custom, rest, labels := NormalizeFields(in, nil, []string{"business_type"})
	if core["contact_name"] != "Ravi" || core["phone"] != "98765 43210" || core["email"] != "r@x.in" || core["message"] != "Need SEO" {
		t.Fatalf("core %v", core)
	}
	if rest["field_9"] != "3" || labels["field_9"] != "Branches" || len(custom) != 0 {
		t.Fatalf("rest %v labels %v custom %v", rest, labels, custom)
	}
	_, _, meta := Flatten(in)
	if meta["form_name"] != "Contact page" || meta["page_url"] != "https://x.in/contact" {
		t.Fatalf("meta %v", meta)
	}
}

func TestNormalizeSimple(t *testing.T) {
	in := map[string]any{"first_name": "Ravi", "last_name": "Kumar", "company": "Ravi Dental", "Your Budget": "50000", "business_type": "Retail", "page_url": "x", "extra": []any{"a", "b"}, "data": map[string]any{"city": "Pune"}}
	core, custom, rest, _ := NormalizeFields(in, map[string]string{"extra": "ignore"}, []string{"business_type"})
	if core["contact_name"] != "Ravi Kumar" || core["name"] != "Ravi Dental" || core["deal_value"] != "50000" || core["city"] != "Pune" {
		t.Fatalf("core %v", core)
	}
	if custom["business_type"] != "Retail" || len(rest) != 0 {
		t.Fatalf("custom %v rest %v", custom, rest)
	}
	if CSVSafe("=1+2") != "'=1+2" || CSVSafe("-5") != "-5" || CSVSafe("abc") != "abc" {
		t.Fatal("csv guard")
	}
}
