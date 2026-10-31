package schema

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// ParametersMap returns the JSON Schema of a tool's Parameters as a map, for
// every form Parameters may take: a map, a jsonorder.Hinted (a schema map that
// carries its source key order), an *OrderedSchema or any other Schema. ok is
// false for nil or an unsupported value. The map is shared, not copied; key
// order is not part of a map, so use OrderedParameters to keep it.
func ParametersMap(p interface{}) (map[string]interface{}, bool) {
	switch v := p.(type) {
	case nil:
		return nil, false
	case map[string]interface{}:
		return v, true
	case jsonorder.Hinted:
		if m, ok := v.V.(map[string]interface{}); ok {
			return m, true
		}
		raw, err := json.Marshal(v.V)
		if err != nil {
			return nil, false
		}
		var m map[string]interface{}
		if json.Unmarshal(raw, &m) != nil || m == nil {
			return nil, false
		}
		return m, true
	case *jsonorder.Hinted:
		if v == nil {
			return nil, false
		}
		return ParametersMap(*v)
	case Schema:
		if v == nil {
			return nil, false
		}
		if val := v.Validator(); val != nil {
			m := val.JSONSchema()
			return m, m != nil
		}
	}
	return nil, false
}

// OrderedParameters converts a jsonorder.Hinted Parameters value to an
// *OrderedSchema, which every provider already sends in source key order.
// ok is false for any other value.
func OrderedParameters(p interface{}) (*OrderedSchema, bool) {
	var h jsonorder.Hinted
	switch v := p.(type) {
	case jsonorder.Hinted:
		h = v
	case *jsonorder.Hinted:
		if v == nil {
			return nil, false
		}
		h = *v
	default:
		return nil, false
	}
	raw, err := h.MarshalJSON()
	if err != nil {
		return nil, false
	}
	s, err := NewOrderedSchema(raw)
	if err != nil {
		return nil, false
	}
	return s, true
}

// OrderedToolParameters returns the schema of a tool whose Parameters is a map
// and whose ParameterKeyOrder carries the schema's source key order, as an
// *OrderedSchema that marshals in that order. ok is false when order is nil or
// not a key order, or parameters is not a schema map.
func OrderedToolParameters(parameters, order interface{}) (*OrderedSchema, bool) {
	if order == nil {
		return nil, false
	}
	m, ok := parameters.(map[string]interface{})
	if !ok || m == nil {
		return nil, false
	}
	if _, isObj := order.(*jsonorder.Object); !isObj {
		return nil, false
	}
	return OrderedParameters(jsonorder.Hinted{V: m, Hint: order})
}
