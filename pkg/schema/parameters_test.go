package schema

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

func hintedSchema(t *testing.T) jsonorder.Hinted {
	t.Helper()
	raw := []byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}},"required":["zeta"]}`)
	hint, ok := jsonorder.Parse(raw)
	if !ok {
		t.Fatal("parse")
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return jsonorder.Hinted{V: v, Hint: hint}
}

func TestParametersMapUnwrapsHinted(t *testing.T) {
	m, ok := ParametersMap(hintedSchema(t))
	if !ok || m["type"] != "object" {
		t.Fatalf("ParametersMap = %v, %v", m, ok)
	}
	if _, ok := ParametersMap(nil); ok {
		t.Fatal("nil must not resolve")
	}
	plain := map[string]interface{}{"type": "object"}
	if got, ok := ParametersMap(plain); !ok || !reflect.DeepEqual(got, plain) {
		t.Fatalf("map passthrough = %v", got)
	}
}

func TestOrderedParametersKeepsHintedOrder(t *testing.T) {
	o, ok := OrderedParameters(hintedSchema(t))
	if !ok {
		t.Fatal("not converted")
	}
	b, _ := o.MarshalJSON()
	want := `{"properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}},"required":["zeta"],"type":"object"}`
	if string(b) == want {
		return
	}
	// Source order of the keys under properties is what matters.
	if i, j := indexOf(string(b), `"zeta"`), indexOf(string(b), `"alpha"`); i < 0 || j < 0 || i > j {
		t.Fatalf("order lost: %s", b)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
