package usage

import "testing"

func TestParseJSONAndTrim(t *testing.T) {
	var d struct {
		Services []struct{ Key string } `json:"services"`
	}
	if !parseJSON("Here:\n```json\n{\"services\":[{\"key\":\"serpapi\"}]}\n```", &d) || d.Services[0].Key != "serpapi" {
		t.Fatal("parse")
	}
	if trimNum(0.0350, 5) != "0.035" || trimNum(1000, 0) != "1000" {
		t.Fatal("trim")
	}
}

func TestModelFallback(t *testing.T) {
	p := Prices{Models: map[string]ModelPrice{"gpt-4o": {2.5, 10}}}
	if m, ok := p.model("openai/gpt-4o"); !ok || m.In != 2.5 {
		t.Fatal("fallback")
	}
}
