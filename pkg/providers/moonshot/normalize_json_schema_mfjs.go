package moonshot

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// mfjsArrayKeys are schema keywords holding an array of subschemas.
var mfjsArrayKeys = []string{"allOf", "anyOf", "oneOf", "prefixItems"}

// mfjsMapKeys are schema keywords holding a map of subschemas.
var mfjsMapKeys = []string{"properties", "patternProperties", "$defs", "dependentSchemas"}

// mfjsSingleKeys are schema keywords holding a single subschema (or boolean).
var mfjsSingleKeys = []string{"additionalProperties", "propertyNames", "items", "contains", "not", "if", "then", "else"}

// NormalizeJSONSchemaForMFJS normalizes a JSON Schema to the subset Moonshot's
// MFJS validator accepts: an `object` root is required, tuple `items` become
// `prefixItems`, and a `type` keyword next to `anyOf` moves into each branch.
// Everything else passes through unchanged. Mirrors TS
// normalizeJsonSchemaForMFJS exactly.
func NormalizeJSONSchemaForMFJS(schema interface{}) (interface{}, error) {
	return normalizeMFJSDefinition(schema, true)
}

func normalizeMFJSDefinition(definition interface{}, isRoot bool) (interface{}, error) {
	def, ok := definition.(map[string]interface{})
	if !ok {
		if isRoot {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: `tool parameters must be a JSON Schema object with type "object" for moonshotai (MFJS)`,
			}
		}
		return definition, nil
	}

	if isRoot {
		if t, _ := def["type"].(string); t != "object" {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: `tool parameters must be a JSON Schema object with type "object" for moonshotai (MFJS)`,
			}
		}
	}

	result := make(map[string]interface{}, len(def))
	for k, v := range def {
		result[k] = v
	}

	// Existing prefixItems stay first (they are positional); a tuple `items`
	// array is appended after them and converted to prefixItems.
	if tuple, ok := result["items"].([]interface{}); ok {
		var prefix []interface{}
		if existing, ok := result["prefixItems"].([]interface{}); ok {
			prefix = append(prefix, existing...)
		}
		for _, item := range tuple {
			normalized, err := normalizeMFJSDefinition(item, false)
			if err != nil {
				return nil, err
			}
			prefix = append(prefix, normalized)
		}
		result["prefixItems"] = prefix
		delete(result, "items")
	} else if itemsMap, ok := result["items"].(map[string]interface{}); ok {
		normalized, err := normalizeMFJSDefinition(itemsMap, false)
		if err != nil {
			return nil, err
		}
		result["items"] = normalized
	}

	// MFJS requires type to live inside anyOf items.
	if parentType, ok := result["type"].(string); ok {
		if anyOf, ok := result["anyOf"].([]interface{}); ok {
			delete(result, "type")
			newAnyOf := make([]interface{}, len(anyOf))
			for i, branch := range anyOf {
				if branchMap, ok := branch.(map[string]interface{}); ok {
					if _, hasType := branchMap["type"]; !hasType {
						merged := make(map[string]interface{}, len(branchMap)+1)
						merged["type"] = parentType
						for k, v := range branchMap {
							merged[k] = v
						}
						newAnyOf[i] = merged
						continue
					}
				}
				newAnyOf[i] = branch
			}
			result["anyOf"] = newAnyOf
		}
	}

	for _, key := range mfjsArrayKeys {
		arr, ok := result[key].([]interface{})
		if !ok {
			continue
		}
		newArr := make([]interface{}, len(arr))
		for i, item := range arr {
			normalized, err := normalizeMFJSDefinition(item, false)
			if err != nil {
				return nil, err
			}
			newArr[i] = normalized
		}
		result[key] = newArr
	}

	for _, key := range mfjsMapKeys {
		m, ok := result[key].(map[string]interface{})
		if !ok {
			continue
		}
		newMap := make(map[string]interface{}, len(m))
		for k, v := range m {
			normalized, err := normalizeMFJSDefinition(v, false)
			if err != nil {
				return nil, err
			}
			newMap[k] = normalized
		}
		result[key] = newMap
	}

	for _, key := range mfjsSingleKeys {
		value, exists := result[key]
		if !exists {
			continue
		}
		if _, isBool := value.(bool); isBool {
			// Booleans pass through unchanged (normalizeDefinition returns the
			// value as-is for non-object, non-root definitions).
			continue
		}
		if m, ok := value.(map[string]interface{}); ok {
			normalized, err := normalizeMFJSDefinition(m, false)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
	}

	return result, nil
}
