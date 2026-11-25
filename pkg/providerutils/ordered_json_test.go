package providerutils

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInjectJSONInstructionSchemaKeepsRawKeyOrder(t *testing.T) {
	raw := json.RawMessage(`{ "type": "object", "properties": {"zeta": {"type":"string"}, "alpha": {"type":"number"}} }`)
	got := InjectJSONInstructionSchema("", raw, InjectJSONInstructionOptions{})
	want := `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}}}`
	if !strings.Contains(got, want) {
		t.Fatalf("raw order lost: %s", got)
	}
	// A map has no order: keys are sorted (deterministic).
	got = InjectJSONInstructionSchema("", map[string]interface{}{"b": 1, "a": 2}, InjectJSONInstructionOptions{})
	if !strings.Contains(got, `{"a":2,"b":1}`) {
		t.Fatalf("map not sorted: %s", got)
	}
}

func TestBatchJSONLLineKeyOrder(t *testing.T) {
	b, err := BatchJSONLLine("r1", "POST", "/v1/responses", map[string]interface{}{"model": "m"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"custom_id":"r1","method":"POST","url":"/v1/responses","body":{"model":"m"}}` {
		t.Fatalf("got %s", b)
	}
}

func TestArgumentsJSONText(t *testing.T) {
	args := map[string]interface{}{"b": float64(1), "a": "x"}
	if got := ArgumentsJSONText(args, `{ "b": 1, "a": "x" }`); got != `{"b":1,"a":"x"}` {
		t.Fatalf("wire order lost: %s", got)
	}
	// Stale raw text (arguments changed after parsing) must not win.
	if got := ArgumentsJSONText(args, `{"b":2,"a":"x"}`); got != `{"a":"x","b":1}` {
		t.Fatalf("stale raw used: %s", got)
	}
	if got := ArgumentsJSONText(nil, ""); got != `{}` {
		t.Fatalf("nil args: %s", got)
	}
}

func TestOrderLike(t *testing.T) {
	raw := json.RawMessage(`{"z":1,"a":{"y":1,"b":2},"l":[{"q":1,"c":2}]}`)
	v := DecodeRawJSONObject(raw)
	v["added"] = true
	b, err := json.Marshal(OrderLike(v, raw))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"z":1,"a":{"y":1,"b":2},"l":[{"q":1,"c":2}],"added":true}` {
		t.Fatalf("got %s", b)
	}
}
