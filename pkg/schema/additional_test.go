package schema

import (
	"reflect"
	"testing"
)

func TestHelperFunctions(t *testing.T) {
	if types, ok := schemaTypes("string"); !ok || len(types) != 1 || types[0] != "string" {
		t.Fatalf("schemaTypes(string) = %v ok=%v", types, ok)
	}
	if _, ok := schemaTypes(""); ok {
		t.Fatal("schemaTypes(empty) should be false")
	}

	vals, ok := interfaceSlice([]string{"a", "b"})
	if !ok || len(vals) != 2 {
		t.Fatalf("interfaceSlice([]string) failed: vals=%v ok=%v", vals, ok)
	}
	if _, ok := interfaceSlice(123); ok {
		t.Fatal("interfaceSlice(non-slice) should fail")
	}

	if out, ok := stringSlice([]interface{}{"x", "y"}); !ok || len(out) != 2 {
		t.Fatalf("stringSlice([]interface{}) failed: out=%v ok=%v", out, ok)
	}
	if _, ok := stringSlice([]interface{}{"x", 2}); ok {
		t.Fatal("stringSlice should fail for mixed types")
	}
}

func TestValidateJSONTypeAndSchemaValue(t *testing.T) {
	if err := validateJSONType(nil, "null", "$"); err != nil {
		t.Fatalf("validateJSONType(nil,null) error = %v", err)
	}
	if err := validateJSONType("x", "string|number", "$"); err != nil {
		t.Fatalf("union type should pass: %v", err)
	}
	if err := validateJSONType(1.2, "integer", "$"); err == nil {
		t.Fatal("expected integer validation error")
	}
	if !isNumber(1.5) || !isInteger(1) || isInteger(1.25) {
		t.Fatal("number/integer helpers mismatch")
	}

	schema := map[string]interface{}{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"age":  map[string]interface{}{"type": "integer"},
		},
	}
	if err := validateJSONSchemaValue(map[string]interface{}{"name": "alice", "age": 42}, schema, "$"); err != nil {
		t.Fatalf("expected valid schema, got %v", err)
	}
	if err := validateJSONSchemaValue(map[string]interface{}{"age": 42}, schema, "$"); err == nil {
		t.Fatal("expected required property error")
	}
}

type taggedStruct struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Hide  string `json:"-"`
	plain string
}

func TestAsMapStructAndPointer(t *testing.T) {
	v := taggedStruct{Name: "bob", Age: 30, Hide: "x", plain: "y"}
	m, ok := asMap(v)
	if !ok {
		t.Fatal("asMap(struct) failed")
	}
	if m["name"] != "bob" || m["age"] != 30 {
		t.Fatalf("unexpected struct map: %+v", m)
	}
	if _, exists := m["Hide"]; exists {
		t.Fatalf("json - field should be omitted: %+v", m)
	}

	pm, ok := asMap(&v)
	if !ok || pm["name"] != "bob" {
		t.Fatalf("asMap(pointer) failed: %+v ok=%v", pm, ok)
	}

	if _, ok := asMap(map[int]string{1: "x"}); ok {
		t.Fatal("asMap(non-string-key map) should fail")
	}
}

func TestStructValidatorTypeMismatch(t *testing.T) {
	type sample struct{ X string }
	validator := NewStructSchema(reflect.TypeOf(sample{}))
	err := validator.Validate(struct{ Y int }{Y: 1})
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
}
