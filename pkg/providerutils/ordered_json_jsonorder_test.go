package providerutils

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// OrderedMap is built on jsonorder: given keys first, the rest sorted, and
// values serialized like JSON.stringify (no HTML escaping of <, > and &) when
// the outer encoder does not escape either (json.Marshal re-escapes a
// Marshaler's output; jsonstr and the SDK's request encoders do not).
func TestOrderedMapUsesJSONOrderSemantics(t *testing.T) {
	b, err := jsonstr.Marshal(OrderedMap{
		Keys: []string{"b", "a", "missing"},
		Vals: map[string]interface{}{"a": "<x & y>", "b": 1, "d": map[string]interface{}{"z": 1, "y": 2}, "c": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"b":1,"a":"<x & y>","c":true,"d":{"y":2,"z":1}}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	ordered := OrderLike(map[string]interface{}{"x": 1, "w": map[string]interface{}{"q": 1, "p": 2}, "extra": 0},
		json.RawMessage(`{"w":{"q":0,"p":0},"x":0}`))
	b, err = jsonstr.Marshal(ordered)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"w":{"q":1,"p":2},"x":1,"extra":0}`; got != want {
		t.Fatalf("OrderLike got %s, want %s", got, want)
	}
}
