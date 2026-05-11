package schema

import (
	"fmt"
	"reflect"
	"strings"

	playground "github.com/go-playground/validator/v10"
)

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
	return validateJSONSchemaValue(data, v.schema, "$")
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
	if data == nil {
		return fmt.Errorf("$: value is required")
	}
	if v.targetType != nil {
		got := reflect.TypeOf(data)
		if got.Kind() == reflect.Ptr {
			got = got.Elem()
		}
		want := v.targetType
		if want.Kind() == reflect.Ptr {
			want = want.Elem()
		}
		if got != want && got.Kind() != reflect.Map {
			return fmt.Errorf("$: expected %s, got %s", want, got)
		}
	}
	return playground.New().Struct(data)
}

// JSONSchema generates a JSON Schema from the struct type
func (v *StructValidator) JSONSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
	}
}

func validateJSONSchemaValue(value interface{}, schema map[string]interface{}, path string) error {
	if schema == nil {
		return nil
	}
	if enumVals, ok := interfaceSlice(schema["enum"]); ok {
		matched := false
		for _, enumVal := range enumVals {
			if reflect.DeepEqual(value, enumVal) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value %v is not one of %v", path, value, enumVals)
		}
	}
	if required, ok := stringSlice(schema["required"]); ok {
		obj, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s: expected object for required properties", path)
		}
		for _, key := range required {
			if _, exists := obj[key]; !exists {
				return fmt.Errorf("%s.%s: required property is missing", path, key)
			}
		}
	}
	if types, ok := schemaTypes(schema["type"]); ok {
		var lastErr error
		for _, typ := range types {
			if err := validateJSONType(value, typ, path); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			return lastErr
		}
	}
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		obj, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s: expected object", path)
		}
		for key, rawPropSchema := range props {
			propValue, exists := obj[key]
			if !exists {
				continue
			}
			propSchema, ok := rawPropSchema.(map[string]interface{})
			if !ok {
				continue
			}
			if err := validateJSONSchemaValue(propValue, propSchema, path+"."+key); err != nil {
				return err
			}
		}
	}
	if items, ok := schema["items"].(map[string]interface{}); ok {
		rv := reflect.ValueOf(value)
		if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
			for i := 0; i < rv.Len(); i++ {
				if err := validateJSONSchemaValue(rv.Index(i).Interface(), items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func schemaTypes(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil, false
		}
		return []string{v}, true
	default:
		return stringSlice(v)
	}
}

func interfaceSlice(value interface{}) ([]interface{}, bool) {
	switch v := value.(type) {
	case []interface{}:
		return v, true
	case nil:
		return nil, false
	default:
		rv := reflect.ValueOf(value)
		if !rv.IsValid() || rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return nil, false
		}
		out := make([]interface{}, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, rv.Index(i).Interface())
		}
		return out, true
	}
}

func validateJSONType(value interface{}, typ, path string) error {
	if value == nil {
		if typ == "null" {
			return nil
		}
		return fmt.Errorf("%s: expected %s, got null", path, typ)
	}
	switch typ {
	case "object":
		if _, ok := asMap(value); !ok {
			return fmt.Errorf("%s: expected object, got %T", path, value)
		}
	case "array":
		k := reflect.TypeOf(value).Kind()
		if k != reflect.Slice && k != reflect.Array {
			return fmt.Errorf("%s: expected array, got %T", path, value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: expected string, got %T", path, value)
		}
	case "number":
		if !isNumber(value) {
			return fmt.Errorf("%s: expected number, got %T", path, value)
		}
	case "integer":
		if !isInteger(value) {
			return fmt.Errorf("%s: expected integer, got %T", path, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T", path, value)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s: expected null, got %T", path, value)
		}
	default:
		if strings.Contains(typ, "|") {
			for _, part := range strings.Split(typ, "|") {
				if validateJSONType(value, strings.TrimSpace(part), path) == nil {
					return nil
				}
			}
		}
	}
	return nil
}

func asMap(value interface{}) (map[string]interface{}, bool) {
	switch v := value.(type) {
	case map[string]interface{}:
		return v, true
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, false
	}
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, false
		}
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = iter.Value().Interface()
		}
		return out, true
	case reflect.Struct:
		out := make(map[string]interface{}, rv.NumField())
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			field := rt.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := field.Name
			if tag := field.Tag.Get("json"); tag != "" {
				tagName := strings.Split(tag, ",")[0]
				if tagName == "-" {
					continue
				}
				if tagName != "" {
					name = tagName
				}
			}
			out[name] = rv.Field(i).Interface()
		}
		return out, true
	default:
		return nil, false
	}
}

func stringSlice(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func isNumber(value interface{}) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func isInteger(value interface{}) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return value.(float32) == float32(int64(value.(float32)))
	case float64:
		return value.(float64) == float64(int64(value.(float64)))
	default:
		return false
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
