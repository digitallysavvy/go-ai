package schema

import (
	"encoding/json"
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

// ApplyDefaults returns a copy of value with JSON Schema object defaults applied.
// It is intentionally conservative: unsupported schema constructs leave the
// original value unchanged, while object properties and array items are handled
// recursively. This mirrors TypeScript schema parsers that can normalize context
// before callbacks receive it.
func ApplyDefaults(value interface{}, schema Schema) interface{} {
	if schema == nil {
		return value
	}
	root := schema.Validator().JSONSchema()
	return applyDefaultsWithRoot(value, root, root)
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

// validateJSONSchemaValue is the package's entry point for JSON Schema
// validation. The schema passed here also serves as the "document root" for
// resolving local "#/$defs/..." / "#/definitions/..." $ref pointers found
// anywhere in the schema, including inside properties/items/allOf/anyOf/etc
// (pkg/ai hoists $defs to the document root for exactly this reason -- see
// output.go's ResponseFormat and object.go's hoistSchemaDefs).
func validateJSONSchemaValue(value interface{}, schema map[string]interface{}, path string) error {
	return validateSchemaValue(value, schema, path, schema)
}

// validateSchemaValue is the recursive validator. root is the top-level
// schema document (constant across a single Validate call) used to resolve
// $ref; sch is the (sub)schema being applied at path.
func validateSchemaValue(value interface{}, sch map[string]interface{}, path string, root map[string]interface{}) error {
	if sch == nil {
		return nil
	}

	// $ref: resolve (following chained local refs) and validate against the
	// resolved schema. Keywords alongside $ref in sch are still evaluated
	// below (draft 2020-12 semantics); draft-07 schemas rarely mix $ref with
	// siblings, so this is a superset-compatible behavior.
	if refVal, ok := sch["$ref"].(string); ok && refVal != "" {
		resolved, err := resolveRefChain(root, refVal)
		if err != nil {
			return fmt.Errorf("%s: $ref: %s", path, err.Error())
		}
		if resolved != nil {
			if err := validateSchemaValue(value, resolved, path, root); err != nil {
				return err
			}
		}
	}

	if constVal, hasConst := sch["const"]; hasConst {
		if !jsonValuesEqual(value, constVal) {
			return fmt.Errorf("%s: const: value %v does not equal %v", path, value, constVal)
		}
	}

	if enumVals, ok := interfaceSlice(sch["enum"]); ok {
		matched := false
		for _, enumVal := range enumVals {
			if jsonValuesEqual(value, enumVal) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: enum: value %v is not one of %v", path, value, enumVals)
		}
	}

	if required, ok := stringSlice(sch["required"]); ok {
		obj, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s: required: expected object for required properties", path)
		}
		for _, key := range required {
			if _, exists := obj[key]; !exists {
				return fmt.Errorf("%s.%s: required: property is missing", path, key)
			}
		}
	}

	if types, ok := schemaTypes(sch["type"]); ok {
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

	if props, ok := sch["properties"].(map[string]interface{}); ok {
		obj, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s: properties: expected object", path)
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
			if err := validateSchemaValue(propValue, propSchema, path+"."+key, root); err != nil {
				return err
			}
		}
	}

	if obj, ok := asMap(value); ok {
		if err := validateObjectConstraints(obj, sch, path, root); err != nil {
			return err
		}
	}

	if s, ok := value.(string); ok {
		if err := validateStringConstraints(s, sch, path); err != nil {
			return err
		}
	}

	if isNumber(value) {
		if err := validateNumberConstraints(value, sch, path); err != nil {
			return err
		}
	}

	if rv := reflect.ValueOf(value); rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
		if err := validateArrayConstraints(value, sch, path, root); err != nil {
			return err
		}
	}

	if allOf, ok := interfaceSlice(sch["allOf"]); ok {
		for i, rawSubschema := range allOf {
			subschema, ok := rawSubschema.(map[string]interface{})
			if !ok {
				continue
			}
			if err := validateSchemaValue(value, subschema, fmt.Sprintf("%s.allOf[%d]", path, i), root); err != nil {
				return err
			}
		}
	}

	if anyOf, ok := interfaceSlice(sch["anyOf"]); ok && len(anyOf) > 0 {
		matched := false
		var lastErr error
		for i, rawSubschema := range anyOf {
			subschema, ok := rawSubschema.(map[string]interface{})
			if !ok {
				continue
			}
			if err := validateSchemaValue(value, subschema, fmt.Sprintf("%s.anyOf[%d]", path, i), root); err != nil {
				lastErr = err
				continue
			}
			matched = true
			break
		}
		if !matched {
			if lastErr != nil {
				return fmt.Errorf("%s: anyOf: value does not match any subschema (last error: %s)", path, lastErr.Error())
			}
			return fmt.Errorf("%s: anyOf: value does not match any subschema", path)
		}
	}

	if oneOf, ok := interfaceSlice(sch["oneOf"]); ok && len(oneOf) > 0 {
		matches := 0
		for i, rawSubschema := range oneOf {
			subschema, ok := rawSubschema.(map[string]interface{})
			if !ok {
				continue
			}
			if err := validateSchemaValue(value, subschema, fmt.Sprintf("%s.oneOf[%d]", path, i), root); err == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s: oneOf: value must match exactly one subschema, matched %d", path, matches)
		}
	}

	if notSchema, ok := sch["not"].(map[string]interface{}); ok {
		if err := validateSchemaValue(value, notSchema, path, root); err == nil {
			return fmt.Errorf("%s: not: value must not match the schema", path)
		}
	}

	return nil
}

// applyDefaultsWithRoot mirrors zod's .default()-filling during parse. root
// is the top-level schema document (constant across a single ApplyDefaults
// call), used to resolve $ref/$defs the same way validateSchemaValue does.
func applyDefaultsWithRoot(value interface{}, sch map[string]interface{}, root map[string]interface{}) interface{} {
	if sch == nil {
		return value
	}

	if refVal, ok := sch["$ref"].(string); ok && refVal != "" {
		if resolved, err := resolveRefChain(root, refVal); err == nil && resolved != nil {
			return applyDefaultsWithRoot(value, resolved, root)
		}
	}

	if props, ok := sch["properties"].(map[string]interface{}); ok {
		obj, ok := asMap(value)
		if !ok {
			return value
		}
		out := make(map[string]interface{}, len(obj)+len(props))
		for k, v := range obj {
			out[k] = v
		}
		for key, rawPropSchema := range props {
			propSchema, ok := rawPropSchema.(map[string]interface{})
			if !ok {
				continue
			}
			if existing, exists := out[key]; exists {
				out[key] = applyDefaultsWithRoot(existing, propSchema, root)
				continue
			}
			if def, ok := schemaDefault(propSchema, root); ok {
				out[key] = cloneJSONValue(def)
			}
		}
		return out
	}

	if prefixItems, ok := interfaceSlice(sch["prefixItems"]); ok && len(prefixItems) > 0 {
		rv := reflect.ValueOf(value)
		if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
			restSchema, _ := sch["items"].(map[string]interface{})
			out := make([]interface{}, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				elem := rv.Index(i).Interface()
				switch {
				case i < len(prefixItems):
					if sub, ok := prefixItems[i].(map[string]interface{}); ok {
						out[i] = applyDefaultsWithRoot(elem, sub, root)
					} else {
						out[i] = elem
					}
				case restSchema != nil:
					out[i] = applyDefaultsWithRoot(elem, restSchema, root)
				default:
					out[i] = elem
				}
			}
			return out
		}
	}

	if items, ok := sch["items"].(map[string]interface{}); ok {
		rv := reflect.ValueOf(value)
		if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
			out := make([]interface{}, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = applyDefaultsWithRoot(rv.Index(i).Interface(), items, root)
			}
			return out
		}
	}

	return value
}

func cloneJSONValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, val := range v {
			out[key] = cloneJSONValue(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, val := range v {
			out[i] = cloneJSONValue(val)
		}
		return out
	default:
		return v
	}
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
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

func isInteger(value interface{}) bool {
	switch v := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return v == float32(int64(v))
	case float64:
		return v == float64(int64(v))
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return true
		}
		f, ok := toFloat64(v)
		return ok && f == float64(int64(f))
	default:
		return false
	}
}

// toFloat64 converts any JSON-decodable numeric Go type to float64. JSON
// numbers decode to float64 via encoding/json by default, but a decoder
// configured with UseNumber() (e.g. pkg/mcp's JSON-RPC decoding, which
// preserves precision for large integers) instead produces json.Number, and
// callers may also pass Go int/uint literals (schema maps built in Go
// code), so all three families are accepted.
func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// toInt converts a numeric schema keyword value (e.g. "minLength": 3) to an
// int, truncating any fractional component.
func toInt(value interface{}) (int, bool) {
	f, ok := toFloat64(value)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// jsonValuesEqual compares two decoded JSON values for equality, treating
// numeric values by numeric equality regardless of their concrete Go type
// (matching JS/JSON's single "number" type, which const/enum comparisons
// rely on -- e.g. a schema literal built as `int(1)` must equal a decoded
// `float64(1)` tool-call argument).
func jsonValuesEqual(a, b interface{}) bool {
	if af, aok := toFloat64(a); aok {
		if bf, bok := toFloat64(b); bok {
			return af == bf
		}
	}
	return reflect.DeepEqual(a, b)
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
