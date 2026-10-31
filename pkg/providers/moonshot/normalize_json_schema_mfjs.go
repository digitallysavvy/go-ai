package moonshot

import (
	"regexp"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// mfjsArrayKeys are schema keywords holding an array of subschemas.
var mfjsArrayKeys = []string{"allOf", "anyOf", "oneOf", "prefixItems"}

// mfjsMapKeys are schema keywords holding a map of subschemas.
var mfjsMapKeys = []string{"properties", "patternProperties", "$defs", "dependentSchemas"}

// mfjsSingleKeys are schema keywords holding a single subschema (or boolean).
var mfjsSingleKeys = []string{"additionalProperties", "propertyNames", "items", "contains", "not", "if", "then", "else"}

const mfjsRootError = `tool and response schemas must be a JSON Schema object with type "object" for moonshotai (MFJS)`

// NormalizeJSONSchemaForMFJS normalizes a JSON Schema to the subset Moonshot's
// MFJS validator accepts: an `object` root is required, tuple `items` become
// `prefixItems`, and a `type` keyword next to `anyOf` moves into each branch below the root. A root
// union of objects (anyOf/oneOf of object schemas, no root type) is sent with
// type "object".
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
				Functionality: mfjsRootError,
			}
		}
		return definition, nil
	}

	// A root union of objects (Zod z.union / discriminatedUnion of objects) is
	// an object; MFJS only needs the root to say so.
	if _, hasType := def["type"]; isRoot && !hasType && isMFJSObjectUnion(def) {
		withType := make(map[string]interface{}, len(def)+1)
		for k, v := range def {
			withType[k] = v
		}
		withType["type"] = "object"
		def = withType
	}

	if isRoot {
		if t, _ := def["type"].(string); t != "object" {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: mfjsRootError,
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

	// MFJS requires type to live inside anyOf items, except at the root, which
	// must keep type "object" (moving it leaves a typeless root that 400s).
	if parentType, ok := result["type"].(string); ok && !isRoot {
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

// isMFJSObjectUnion reports whether every anyOf/oneOf on the root is a
// non-empty union of object schemas. Local $refs (#/$defs/X, #/definitions/X)
// are resolved one hop. A nullable branch (type: ["object", "null"]) is not an
// object, so such unions keep failing the root check.
func isMFJSObjectUnion(def map[string]interface{}) bool {
	found := false
	for _, key := range []string{"anyOf", "oneOf"} {
		value, exists := def[key]
		if !exists {
			continue
		}
		found = true
		branches, ok := value.([]interface{})
		if !ok || len(branches) == 0 {
			return false
		}
		for _, branch := range branches {
			if !isMFJSObjectSchema(branch, def) {
				return false
			}
		}
	}
	return found
}

func isMFJSObjectSchema(branch interface{}, root map[string]interface{}) bool {
	resolved, ok := resolveMFJSLocalRef(branch, root).(map[string]interface{})
	if !ok {
		return false
	}
	t, hasType := resolved["type"]
	if hasType {
		s, _ := t.(string)
		return s == "object"
	}
	_, isMap := resolved["properties"].(map[string]interface{})
	return isMap
}

var mfjsLocalRef = regexp.MustCompile(`^#/(\$defs|definitions)/([^/]+)$`)

func resolveMFJSLocalRef(branch interface{}, root map[string]interface{}) interface{} {
	b, ok := branch.(map[string]interface{})
	if !ok {
		return branch
	}
	ref, ok := b["$ref"].(string)
	if !ok {
		return branch
	}
	m := mfjsLocalRef.FindStringSubmatch(ref)
	if m == nil {
		return branch
	}
	if defs, ok := root[m[1]].(map[string]interface{}); ok {
		return defs[m[2]]
	}
	return branch
}
