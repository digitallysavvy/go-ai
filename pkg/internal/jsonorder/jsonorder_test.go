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

func TestObjectMarshalSetDeleteAndPlain(t *testing.T) {
	var o Object
	if err := json.Unmarshal([]byte(`{"z":1,"a":{"y":[1.5,"<"],"b":null}}`), &o); err != nil {
		t.Fatal(err)
	}
	o.Set("m", true)
	o.Set("z", 2) // existing key keeps its place
	o.Delete("a")
	b, err := json.Marshal(&o)
	if err != nil || string(b) != `{"z":2,"m":true}` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	if keys := o.Keys(); len(keys) != 2 || keys[0] != "z" {
		t.Fatalf("keys = %v", keys)
	}
	v, _ := Parse([]byte(`{"n":3,"o":{"k":[1]}}`))
	plain, _ := Plain(v).(map[string]interface{})
	if plain["n"] != float64(3) {
		t.Fatalf("plain = %#v", plain)
	}
	// Embedded in a struct or map, json.Marshal keeps the order.
	out, _ := json.Marshal(map[string]interface{}{"schema": mustParseObject(t, `{"b":1,"a":2}`)})
	if string(out) != `{"schema":{"b":1,"a":2}}` {
		t.Fatalf("nested = %s", out)
	}
	if err := json.Unmarshal([]byte(`[1]`), &Object{}); err == nil {
		t.Fatal("array accepted as object")
	}
}

func mustParseObject(t *testing.T, raw string) *Object {
	t.Helper()
	v, ok := Parse([]byte(raw))
	o, isObj := v.(*Object)
	if !ok || !isObj {
		t.Fatalf("bad object %s", raw)
	}
	return o
}
