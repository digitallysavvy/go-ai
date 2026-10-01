package schema

import (
	"reflect"
	"testing"
)

func TestNewJSONSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
		},
	}

	validator := NewJSONSchema(schema)

	if validator == nil {
		t.Fatal("expected non-nil validator")
	}
}

func TestJSONSchemaValidator_JSONSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
		},
	}

	validator := NewJSONSchema(schema)
	result := validator.JSONSchema()

	if result == nil {
		t.Fatal("expected non-nil JSON schema")
	}
	if result["type"] != "object" {
		t.Errorf("expected type 'object', got %v", result["type"])
	}
}

func TestJSONSchemaValidator_Validate(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
		},
	}

	validator := NewJSONSchema(schema)

	err := validator.Validate(map[string]interface{}{"name": "John"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestApplyDefaults(t *testing.T) {
	t.Parallel()

	s := NewSimpleJSONSchema(map[string]interface{}{
		"type":     "object",
		"required": []string{"apiKey", "region"},
		"properties": map[string]interface{}{
			"apiKey": map[string]interface{}{"type": "string"},
			"region": map[string]interface{}{
				"type":    "string",
				"default": "us-east-1",
			},
			"nested": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"enabled": map[string]interface{}{
						"type":    "boolean",
						"default": true,
					},
				},
			},
		},
	})

	original := map[string]interface{}{"apiKey": "secret", "nested": map[string]interface{}{}}
	got := ApplyDefaults(original, s)
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("ApplyDefaults() = %T, want map", got)
	}
	if obj["region"] != "us-east-1" {
		t.Fatalf("region default = %v, want us-east-1", obj["region"])
	}
	nested, ok := obj["nested"].(map[string]interface{})
	if !ok || nested["enabled"] != true {
		t.Fatalf("nested default = %+v, want enabled=true", obj["nested"])
	}
	if _, exists := original["region"]; exists {
		t.Fatal("ApplyDefaults mutated original context")
	}
	if err := s.Validator().Validate(got); err != nil {
		t.Fatalf("defaulted context should validate: %v", err)
	}
}

func TestNewStructSchema(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	validator := NewStructSchema(reflect.TypeOf(TestStruct{}))

	if validator == nil {
		t.Fatal("expected non-nil validator")
	}
}

func TestStructValidator_JSONSchema(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Name string `json:"name"`
	}

	validator := NewStructSchema(reflect.TypeOf(TestStruct{}))
	result := validator.JSONSchema()

	if result == nil {
		t.Fatal("expected non-nil JSON schema")
	}
	if result["type"] != "object" {
		t.Errorf("expected type 'object', got %v", result["type"])
	}
}

func TestStructValidator_Validate(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Name string `json:"name"`
	}

	validator := NewStructSchema(reflect.TypeOf(TestStruct{}))

	err := validator.Validate(TestStruct{Name: "John"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewSimpleJSONSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "object",
	}

	simpleSchema := NewSimpleJSONSchema(schema)

	if simpleSchema == nil {
		t.Fatal("expected non-nil schema")
	}
}

func TestSimpleJSONSchema_Validator(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
		},
	}

	simpleSchema := NewSimpleJSONSchema(schema)
	validator := simpleSchema.Validator()

	if validator == nil {
		t.Fatal("expected non-nil validator")
	}

	// Check that the validator returns the correct JSON schema
	jsonSchema := validator.JSONSchema()
	if jsonSchema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", jsonSchema["type"])
	}
}

func TestNewSimpleStructSchema(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Name string `json:"name"`
	}

	simpleSchema := NewSimpleStructSchema(reflect.TypeOf(TestStruct{}))

	if simpleSchema == nil {
		t.Fatal("expected non-nil schema")
	}
}

func TestSimpleStructSchema_Validator(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Name string `json:"name"`
	}

	simpleSchema := NewSimpleStructSchema(reflect.TypeOf(TestStruct{}))
	validator := simpleSchema.Validator()

	if validator == nil {
		t.Fatal("expected non-nil validator")
	}

	// Check that the validator returns a JSON schema
	jsonSchema := validator.JSONSchema()
	if jsonSchema == nil {
		t.Fatal("expected non-nil JSON schema")
	}
}

func TestJSONSchemaValidator_ComplexSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
			"age": map[string]interface{}{
				"type":    "integer",
				"minimum": 0,
			},
			"email": map[string]interface{}{
				"type":   "string",
				"format": "email",
			},
			"tags": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"required": []string{"name", "email"},
	}

	validator := NewJSONSchema(schema)
	result := validator.JSONSchema()

	// Verify schema structure is preserved
	props, ok := result["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	nameProp, ok := props["name"].(map[string]interface{})
	if !ok {
		t.Fatal("expected name property to be a map")
	}

	if nameProp["type"] != "string" {
		t.Errorf("expected name type 'string', got %v", nameProp["type"])
	}
}

func TestSimpleJSONSchema_ValidatorInterface(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "string",
	}

	simpleSchema := NewSimpleJSONSchema(schema)

	// Verify it implements the Schema interface
	var s Schema = simpleSchema
	validator := s.Validator()

	if validator == nil {
		t.Error("expected validator from Schema interface")
	}
}

func TestSimpleStructSchema_ValidatorInterface(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Field string
	}

	simpleSchema := NewSimpleStructSchema(reflect.TypeOf(TestStruct{}))

	// Verify it implements the Schema interface
	var s Schema = simpleSchema
	validator := s.Validator()

	if validator == nil {
		t.Error("expected validator from Schema interface")
	}
}

func TestJSONSchemaValidator_ValidatorInterface(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{
		"type": "number",
	}

	validator := NewJSONSchema(schema)

	// Verify it implements the Validator interface
	var v Validator = validator

	// Should not panic
	_ = v.JSONSchema()
	_ = v.Validate(123)
}

func TestStructValidator_ValidatorInterface(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Value int
	}

	validator := NewStructSchema(reflect.TypeOf(TestStruct{}))

	// Verify it implements the Validator interface
	var v Validator = validator

	// Should not panic
	_ = v.JSONSchema()
	_ = v.Validate(TestStruct{Value: 42})
}

func TestJSONSchemaValidator_EmptySchema(t *testing.T) {
	t.Parallel()

	schema := map[string]interface{}{}

	validator := NewJSONSchema(schema)

	if validator == nil {
		t.Fatal("expected non-nil validator for empty schema")
	}

	result := validator.JSONSchema()
	if result == nil {
		t.Error("expected non-nil result")
	}
	if len(result) != 0 {
		t.Error("expected empty schema to be preserved")
	}
}

func TestStructValidator_PointerType(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Name *string `json:"name"`
	}

	validator := NewStructSchema(reflect.TypeOf(TestStruct{}))

	if validator == nil {
		t.Fatal("expected non-nil validator")
	}

	// Placeholder implementation should still work
	err := validator.Validate(TestStruct{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStructValidator_NestedStruct(t *testing.T) {
	t.Parallel()

	type Address struct {
		City string `json:"city"`
	}

	type Person struct {
		Name    string  `json:"name"`
		Address Address `json:"address"`
	}

	validator := NewStructSchema(reflect.TypeOf(Person{}))

	if validator == nil {
		t.Fatal("expected non-nil validator")
	}

	err := validator.Validate(Person{
		Name:    "John",
		Address: Address{City: "NYC"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestJSONSchemaValidator_NullTypeAndNullableUnion(t *testing.T) {
	t.Parallel()

	if err := NewJSONSchema(map[string]interface{}{"type": "null"}).Validate(nil); err != nil {
		t.Fatalf("null type should accept nil: %v", err)
	}
	if err := NewJSONSchema(map[string]interface{}{"type": []interface{}{"string", "null"}}).Validate(nil); err != nil {
		t.Fatalf("nullable union should accept nil: %v", err)
	}
	if err := NewJSONSchema(map[string]interface{}{"items": map[string]interface{}{"type": "string"}}).Validate(nil); err != nil {
		t.Fatalf("items without array type should ignore nil instead of panicking: %v", err)
	}
}
