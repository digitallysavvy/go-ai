package schema

import (
	"encoding/json"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// OrderedSchema is a JSON Schema that keeps the key order of its JSON source.
//
// JavaScript objects keep insertion order, so the TS SDK sends a schema to the
// model exactly as it was written or received (an MCP server's tool schema, a
// raw JSON schema string). A Go map sorts its keys, and the property order of
// a schema shapes the order of the model's JSON answer. Build an OrderedSchema
// from the JSON text when that order matters; it marshals back in the same
// order (json.Marshal, and so every request encoder that marshals the value).
//
// An OrderedSchema is a Schema and a Validator, and JSONSchema() returns the
// same schema as a map for code that needs one (key order is lost there).
type OrderedSchema struct {
	root *jsonorder.Object
	m    map[string]interface{}
}

// NewOrderedSchema parses raw JSON Schema text into an OrderedSchema. The text
// must be a JSON object.
func NewOrderedSchema(raw []byte) (*OrderedSchema, error) {
	v, ok := jsonorder.Parse(raw)
	if !ok {
		return nil, errors.New("schema: invalid JSON")
	}
	root, isObj := v.(*jsonorder.Object)
	if !isObj {
		return nil, errors.New("schema: a JSON Schema must be an object")
	}
	return &OrderedSchema{root: root, m: root.Map()}, nil
}

// Validator returns the schema itself.
func (s *OrderedSchema) Validator() Validator { return s }

// Validate validates data against the schema.
func (s *OrderedSchema) Validate(data interface{}) error {
	return validateJSONSchemaValue(data, s.m, "$")
}

// JSONSchema returns the schema as a map (key order is lost); nil for a nil
// or zero OrderedSchema.
func (s *OrderedSchema) JSONSchema() map[string]interface{} {
	if s == nil {
		return nil
	}
	return s.m
}

// MarshalJSON writes the schema in its source key order; {} for a nil or
// zero OrderedSchema.
func (s *OrderedSchema) MarshalJSON() ([]byte, error) {
	if s == nil || s.root == nil {
		return []byte("{}"), nil
	}
	return s.root.MarshalJSON()
}

// Keys returns the top-level keys in source order.
func (s *OrderedSchema) Keys() []string { return s.root.Keys() }

// MarshalSchema returns the JSON text of a schema for the wire. An
// *OrderedSchema keeps its key order; every other schema marshals its
// JSONSchema() map (sorted keys). ok is false when v is not a schema.
func MarshalSchema(v interface{}) (data []byte, ok bool, err error) {
	switch s := v.(type) {
	case *OrderedSchema:
		if s == nil {
			return nil, false, nil
		}
		data, err = s.MarshalJSON()
		return data, true, err
	case *SimpleStructSchema, *StructValidator:
		if isNilValue(s) {
			return nil, false, nil
		}
		data, err = json.Marshal(s)
		return data, true, err
	case Schema:
		// A typed nil schema or validator is no schema (TS
		// asSchema(undefined)); its methods must not be called.
		if isNilValue(s) {
			return nil, false, nil
		}
		val := s.Validator()
		if isNilValue(val) {
			return nil, false, nil
		}
		data, err = json.Marshal(val.JSONSchema())
		return data, true, err
	case Validator:
		if isNilValue(s) {
			return nil, false, nil
		}
		data, err = json.Marshal(s.JSONSchema())
		return data, true, err
	}
	return nil, false, nil
}

// MarshalJSON writes the JSON Schema, so a schema value used where a JSON
// Schema is expected (a tool's Parameters) serializes as the schema.
// A nil or zero value, which is no schema, writes {}.
func (s *SimpleJSONSchema) MarshalJSON() ([]byte, error) {
	if s == nil || s.validator == nil {
		return []byte("{}"), nil
	}
	return s.validator.MarshalJSON()
}

// MarshalJSON writes the JSON Schema; {} for a nil validator.
func (v *JSONSchemaValidator) MarshalJSON() ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	return marshalSchemaMap(v.schema)
}

// MarshalJSON writes the JSON Schema in its generated key order (struct
// field order, as the TS zod schema keeps its keys).
func (s *SimpleStructSchema) MarshalJSON() ([]byte, error) {
	if s == nil || s.validator == nil {
		return []byte("{}"), nil
	}
	return s.validator.MarshalJSON()
}

// MarshalJSON writes the JSON Schema in its generated key order.
func (v *StructValidator) MarshalJSON() ([]byte, error) {
	if raw := v.orderedJSONSchema(); raw != nil {
		return raw, nil
	}
	return []byte("{}"), nil
}

func marshalSchemaMap(m map[string]interface{}) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}
