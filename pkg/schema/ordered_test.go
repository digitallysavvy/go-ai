package schema

import (
	"encoding/json"
	"testing"
)

func TestOrderedSchemaKeepsSourceKeyOrder(t *testing.T) {
	raw := `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"},"mid":{"enum":["b","a"]}},"required":["zeta","alpha"]}`
	s, err := NewOrderedSchema([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != raw {
		t.Fatalf("marshal = %s, want %s", got, raw)
	}
	// Nested in another value (a request body) keeps the order too.
	body, _ := json.Marshal(map[string]interface{}{"parameters": s})
	if string(body) != `{"parameters":`+raw+`}` {
		t.Fatalf("nested = %s", body)
	}
	if err := s.Validator().Validate(map[string]interface{}{"zeta": "x", "alpha": 1.0}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := s.Validator().Validate(map[string]interface{}{"zeta": 1.0}); err == nil {
		t.Fatal("expected a validation error")
	}
	if keys := s.Keys(); len(keys) != 3 || keys[0] != "type" || keys[2] != "required" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestNewOrderedSchemaRejectsNonObjects(t *testing.T) {
	for _, raw := range []string{`[]`, `"x"`, `{`, ``} {
		if _, err := NewOrderedSchema([]byte(raw)); err == nil {
			t.Fatalf("NewOrderedSchema(%q) = nil error", raw)
		}
	}
}

// A schema used as a tool's Parameters must serialize as the schema, not {}.
func TestSchemaTypesMarshalAsJSONSchema(t *testing.T) {
	m := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}}
	want := `{"properties":{"a":{"type":"string"}},"type":"object"}`
	for name, v := range map[string]interface{}{
		"SimpleJSONSchema":    NewSimpleJSONSchema(m),
		"JSONSchemaValidator": NewJSONSchema(m),
	} {
		got, err := json.Marshal(v)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %s, %v; want %s", name, got, err, want)
		}
	}
	got, _ := json.Marshal(NewSimpleJSONSchema(nil))
	if string(got) != `{}` {
		t.Fatalf("nil schema = %s", got)
	}
}

func TestMarshalSchema(t *testing.T) {
	o, _ := NewOrderedSchema([]byte(`{"b":1,"a":2}`))
	got, ok, err := MarshalSchema(o)
	if !ok || err != nil || string(got) != `{"b":1,"a":2}` {
		t.Fatalf("ordered: %s %v %v", got, ok, err)
	}
	got, ok, err = MarshalSchema(NewSimpleJSONSchema(map[string]interface{}{"b": 1, "a": 2}))
	if !ok || err != nil || string(got) != `{"a":2,"b":1}` {
		t.Fatalf("simple: %s %v %v", got, ok, err)
	}
	if _, ok, _ := MarshalSchema(42); ok {
		t.Fatal("non-schema reported ok")
	}
}
