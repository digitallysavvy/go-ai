package schema

import "reflect"

// Validator validates data against a schema
type Validator interface {
	// Validate validates data against the schema
	// Returns an error if validation fails
	Validate(data interface{}) error

	// JSONSchema returns the JSON Schema representation of this validator
	// This is used when sending schemas to AI providers
	JSONSchema() map[string]interface{}
}

// Schema represents a validation schema
// Can be implemented as JSON Schema or Go struct-based schema
type Schema interface {
	// Validator returns the validator for this schema
	Validator() Validator
}

// JSONSchemaValidator validates using JSON Schema
type JSONSchemaValidator struct {
	schema map[string]interface{}
}

// NewJSONSchema creates a new JSON Schema validator
func NewJSONSchema(schema map[string]interface{}) *JSONSchemaValidator {
	return &JSONSchemaValidator{schema: schema}
}

// Validate validates data against the JSON Schema
func (v *JSONSchemaValidator) Validate(data interface{}) error {
	// TODO: Implement JSON Schema validation using github.com/santhosh-tekuri/jsonschema
	// For now, return nil (will be implemented in Phase 2)
	return nil
}

// JSONSchema returns the JSON Schema
func (v *JSONSchemaValidator) JSONSchema() map[string]interface{} {
	return v.schema
}

// StructValidator validates using Go struct tags
type StructValidator struct {
	targetType reflect.Type
}

// NewStructSchema creates a new struct-based schema validator
func NewStructSchema(targetType reflect.Type) *StructValidator {
	return &StructValidator{targetType: targetType}
}

// Validate validates data against the struct schema
func (v *StructValidator) Validate(data interface{}) error {
	// TODO: Implement struct tag validation using github.com/go-playground/validator
	// For now, return nil (will be implemented in Phase 2)
	return nil
}

// JSONSchema generates a JSON Schema from the struct type
func (v *StructValidator) JSONSchema() map[string]interface{} {
	// TODO: Generate JSON Schema from struct tags
	// For now, return empty schema (will be implemented in Phase 2)
	return map[string]interface{}{
		"type": "object",
	}
}

// SimpleJSONSchema is a simple implementation of Schema
type SimpleJSONSchema struct {
	validator *JSONSchemaValidator
}

// NewSimpleJSONSchema creates a simple JSON Schema
func NewSimpleJSONSchema(schema map[string]interface{}) *SimpleJSONSchema {
	return &SimpleJSONSchema{
		validator: NewJSONSchema(schema),
	}
}

// Validator returns the validator
func (s *SimpleJSONSchema) Validator() Validator {
	return s.validator
}

// SimpleStructSchema is a simple implementation of Schema using structs
type SimpleStructSchema struct {
	validator *StructValidator
}

// NewSimpleStructSchema creates a simple struct schema
func NewSimpleStructSchema(targetType reflect.Type) *SimpleStructSchema {
	return &SimpleStructSchema{
		validator: NewStructSchema(targetType),
	}
}

// Validator returns the validator
func (s *SimpleStructSchema) Validator() Validator {
	return s.validator
}
