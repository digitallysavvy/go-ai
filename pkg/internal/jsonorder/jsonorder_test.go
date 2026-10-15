package jsonorder

import (
	"encoding/json"
	"testing"
)

func TestTextKeepsWireOrderAndDoesNotEscapeHTML(t *testing.T) {
	raw := json.RawMessage(`{ "z": 1, "a": {"y": [1, 2, {"b": true, "a": null}], "x": "<&>"} }`)
	got := Text(raw, nil)
	want := `{"z":1,"a":{"y":[1,2,{"b":true,"a":null}],"x":"<&>"}}`
	if got != want {
		t.Fatalf("Text = %s, want %s", got, want)
	}
}

func TestWithLeadPutsKeyFirstAndInputValueWins(t *testing.T) {
	o := Input(json.RawMessage(`{"code":"x","type":"mine"}`), nil)
	if got := o.WithLead("type", "programmatic-tool-call").String(); got != `{"type":"mine","code":"x"}` {
		t.Fatalf("lead with existing key = %s", got)
	}
	o = Input(json.RawMessage(`{"command":"ls"}`), nil)
	if got := o.WithLead("type", "bash_code_execution").String(); got != `{"type":"bash_code_execution","command":"ls"}` {
		t.Fatalf("lead = %s", got)
	}
}

func TestInputFallsBackToSortedMap(t *testing.T) {
	if got := Text(nil, map[string]interface{}{"b": 1, "a": 2}); got != `{"a":2,"b":1}` {
		t.Fatalf("fallback = %s", got)
	}
	if got := Text(json.RawMessage(`not json`), nil); got != `{}` {
		t.Fatalf("invalid raw = %s", got)
	}
}

func TestHintedRestoresRawKeyOrder(t *testing.T) {
	raw := []byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"integer","enum":[3,1]}},"required":["zeta"]}`)
	hint, ok := Parse(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	// A transform adds a key the hint does not know.
	decoded.(map[string]interface{})["additionalProperties"] = false
	got, err := json.Marshal(Hinted{V: decoded, Hint: hint})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"integer","enum":[3,1]}},"required":["zeta"],"additionalProperties":false}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
