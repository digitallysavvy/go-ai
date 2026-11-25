package provider

import (
	"encoding/json"
	"testing"
)

func TestPortCarryExtractionAnswerJSON(t *testing.T) {
	conf := 0.5
	cases := []struct {
		name string
		in   ExtractionAnswer
		want string
	}{
		{"null value is emitted", ExtractionAnswer{Type: "number"}, `{"type":"number","value":null}`},
		{"refusal carries only the type", ExtractionAnswer{Type: "refusal", Value: "x"}, `{"type":"refusal"}`},
		{"text with source", ExtractionAnswer{Type: "text", Value: "INV-1", Source: &ExtractionSource{Type: "text", Part: 0, Start: 0, End: 5}},
			`{"type":"text","value":"INV-1","source":{"type":"text","part":0,"start":0,"end":5}}`},
		{"file source", ExtractionAnswer{Type: "text", Value: "a", Source: &ExtractionSource{Type: "file", Part: 2}},
			`{"type":"text","value":"a","source":{"type":"file","part":2}}`},
		{"list items", ExtractionAnswer{Type: "list", Value: []string{"Ana"}, Items: []ExtractionListItem{{Confidence: &conf}}},
			`{"type":"list","value":["Ana"],"items":[{"confidence":0.5}]}`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.want {
			t.Errorf("%s: got %s want %s", c.name, b, c.want)
		}
	}
}

func TestPortCarryExtractionFieldIDsOrder(t *testing.T) {
	o := ExtractionCallOptions{
		Fields:     map[string]ExtractionField{"b": {Type: "text"}, "a": {Type: "text"}, "c": {Type: "text"}},
		FieldOrder: []string{"c", "zzz", "b"},
	}
	got := o.FieldIDs()
	want := []string{"c", "b", "a"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("FieldIDs = %v want %v", got, want)
	}
}

func TestPortCarryExtractionFieldTypes(t *testing.T) {
	if got := ExtractionFieldTypes(); len(got) != 7 || got[0] != "text" || got[6] != "list" {
		t.Fatalf("ExtractionFieldTypes = %v", got)
	}
}
