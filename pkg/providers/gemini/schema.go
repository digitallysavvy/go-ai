package gemini

import "encoding/json"

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
func sanitizeResponseJSONSchema(schema interface{}) interface{} {
	m, ok := toSchemaMap(schema)
	if !ok {
		return schema
	}
	return sanitizeSchemaMap(m)
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
