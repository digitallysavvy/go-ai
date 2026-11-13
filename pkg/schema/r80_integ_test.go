package schema

import (
	"reflect"
	"testing"
)

type r80integEmail struct {
	Sender   string   `json:"sender"`
	Priority string   `json:"priority" validate:"required"`
	Points   []string `json:"key_points"`
}

// A struct-tag schema validates the decoded JSON map every caller passes it
// (TS validates the parsed value with the schema). v0.5.2 always failed with
// "validator: (nil map[string]interface {})".
func TestR80integStructValidatorValidatesDecodedMap(t *testing.T) {
	v := NewSimpleStructSchema(reflect.TypeOf(r80integEmail{})).Validator()
	if err := v.Validate(map[string]interface{}{"priority": "high", "key_points": []interface{}{"a"}}); err != nil {
		t.Fatalf("valid map: %v", err)
	}
	if err := v.Validate(map[string]interface{}{"sender": "x"}); err == nil {
		t.Fatal("missing required priority: want an error")
	}
	if err := v.Validate(map[string]interface{}{"priority": 3}); err == nil {
		t.Fatal("priority of the wrong type: want an error")
	}
	if err := v.Validate(r80integEmail{Priority: "low"}); err != nil {
		t.Fatalf("struct value: %v", err)
	}
	if err := v.Validate(&r80integEmail{}); err == nil {
		t.Fatal("struct pointer without priority: want an error")
	}
	if err := v.Validate("text"); err == nil {
		t.Fatal("string: want an error")
	}
	if err := NewStructSchema(reflect.TypeOf(&r80integEmail{})).Validate(map[string]interface{}{"priority": "p"}); err != nil {
		t.Fatalf("pointer target type with a valid map: %v", err)
	}
}
