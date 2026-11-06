package gemini

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// toSchemaMap converts a JSON schema value of any Go shape (map, struct,
// json.RawMessage, ...) into a map. Returns false when the value is not a
// JSON object.
func toSchemaMap(schema interface{}) (map[string]interface{}, bool) {
	switch s := schema.(type) {
	case nil:
		return nil, false
	case map[string]interface{}:
		return s, true
	case json.RawMessage:
		var m map[string]interface{}
		if err := json.Unmarshal(s, &m); err != nil {
			return nil, false
		}
		return m, true
	case []byte:
		var m map[string]interface{}
		if err := json.Unmarshal(s, &m); err != nil {
			return nil, false
		}
		return m, true
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	return m, true
}

// sanitizeResponseJSONSchema recursively replaces `const` with a single-value
// `enum` in the JSON Schema locations supported by Google, because
// `responseJsonSchema` does not support `const`. All other schema properties
// are preserved and the input is never mutated.
// Mirrors TS sanitizeResponseJsonSchema (sanitize-response-json-schema.ts).
//
// A schema.Schema (what ai.GenerateObject passes) is resolved to its JSON
// Schema first. Raw JSON keeps the key order the caller wrote, like the JS
// object TS sanitizes, so `properties` stay in order on the wire.
func sanitizeResponseJSONSchema(schema interface{}) interface{} {
	var hint interface{}
	switch s := schema.(type) {
	case json.RawMessage:
		hint, _ = jsonorder.Parse(s)
	case []byte:
		hint, _ = jsonorder.Parse(s)
	default:
		// An ordered schema comes back as raw JSON and keeps its key order.
		schema = providerutils.ResponseFormatJSONSchemaOrdered(schema)
		if raw, ok := schema.(json.RawMessage); ok {
			hint, _ = jsonorder.Parse(raw)
		}
	}
	m, ok := toSchemaMap(schema)
	if !ok {
		return schema
	}
	out := sanitizeSchemaMap(m)
	if hint != nil {
		return jsonorder.Hinted{V: out, Hint: sanitizedSchemaHint(hint)}
	}
	return out
}

// sanitizedSchemaMovedKeys are the keys TS sanitizeResponseJsonSchema takes
// out of the schema and writes back after the other keys, in this order.
var sanitizedSchemaMovedKeys = []string{"properties", "items", "additionalProperties", "anyOf", "oneOf"}

// sanitizedSchemaHint is the key order of TS sanitizeResponseJsonSchema's
// result for the input tree hint: the input keys except const and the moved
// keys (in input order; an existing enum or $defs keeps its place), then enum
// when it comes from const, then properties, items, additionalProperties,
// anyOf and oneOf. Subschemas get the same order; other values keep theirs.
func sanitizedSchemaHint(hint interface{}) interface{} {
	o, ok := hint.(*jsonorder.Object)
	if !ok {
		return hint
	}
	out := jsonorder.New()
	moved := map[string]bool{"const": true}
	for _, k := range sanitizedSchemaMovedKeys {
		moved[k] = true
	}
	for _, k := range o.Keys() {
		if moved[k] {
			continue
		}
		v, _ := o.Get(k)
		if k == "$defs" {
			v = sanitizedDefinitionsHint(v)
		}
		out.Set(k, v)
	}
	if c, ok := o.Get("const"); ok {
		// Set keeps an existing enum's position.
		out.Set("enum", []interface{}{c})
	}
	for _, k := range sanitizedSchemaMovedKeys {
		v, ok := o.Get(k)
		if !ok {
			continue
		}
		switch k {
		case "properties":
			v = sanitizedDefinitionsHint(v)
		case "items", "anyOf", "oneOf":
			if list, isList := v.([]interface{}); isList {
				hints := make([]interface{}, len(list))
				for i, item := range list {
					hints[i] = sanitizedSchemaHint(item)
				}
				v = hints
			} else {
				v = sanitizedSchemaHint(v)
			}
		default:
			v = sanitizedSchemaHint(v)
		}
		out.Set(k, v)
	}
	return out
}

// sanitizedDefinitionsHint orders a properties / $defs map hint: entries in
// input order, each a sanitized subschema.
func sanitizedDefinitionsHint(hint interface{}) interface{} {
	o, ok := hint.(*jsonorder.Object)
	if !ok {
		return hint
	}
	out := jsonorder.New()
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		out.Set(k, sanitizedSchemaHint(v))
	}
	return out
}

func sanitizeSchemaMap(schema map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(schema))
	for k, v := range schema {
		switch k {
		case "const", "properties", "items", "additionalProperties", "anyOf", "oneOf", "$defs":
			continue
		}
		result[k] = v
	}

	if c, ok := schema["const"]; ok {
		result["enum"] = []interface{}{c}
	}
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		result["properties"] = sanitizeDefinitions(props)
	} else if v, ok := schema["properties"]; ok && v != nil {
		result["properties"] = v
	}
	if items, ok := schema["items"]; ok && items != nil {
		if list, isList := items.([]interface{}); isList {
			out := make([]interface{}, len(list))
			for i, item := range list {
				out[i] = sanitizeDefinition(item)
			}
			result["items"] = out
		} else {
			result["items"] = sanitizeDefinition(items)
		}
	}
	if ap, ok := schema["additionalProperties"]; ok && ap != nil {
		result["additionalProperties"] = sanitizeDefinition(ap)
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		if list, ok := schema[key].([]interface{}); ok {
			out := make([]interface{}, len(list))
			for i, item := range list {
				out[i] = sanitizeDefinition(item)
			}
			result[key] = out
		} else if v, ok := schema[key]; ok && v != nil {
			result[key] = v
		}
	}
	if defs, ok := schema["$defs"].(map[string]interface{}); ok {
		result["$defs"] = sanitizeDefinitions(defs)
	} else if v, ok := schema["$defs"]; ok && v != nil {
		result["$defs"] = v
	}
	return result
}

func sanitizeDefinitions(defs map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(defs))
	for name, def := range defs {
		out[name] = sanitizeDefinition(def)
	}
	return out
}

func sanitizeDefinition(def interface{}) interface{} {
	if b, ok := def.(bool); ok {
		return b
	}
	if m, ok := toSchemaMap(def); ok {
		return sanitizeSchemaMap(m)
	}
	return def
}
