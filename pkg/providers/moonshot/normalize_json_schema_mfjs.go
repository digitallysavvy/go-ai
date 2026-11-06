package moonshot

import (
	"regexp"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
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
// normalizeJsonSchemaForMFJS exactly. A map schema gives a map; an ordered
// schema object (from the schema's JSON text) gives an ordered object whose
// keys are where the TS object spread and assignments put them.
func NormalizeJSONSchemaForMFJS(schema interface{}) (interface{}, error) {
	_, isMap := schema.(map[string]interface{})
	out, err := normalizeMFJSDefinition(mfjsOrdered(schema), true)
	if err != nil {
		return nil, err
	}
	if isMap {
		return mfjsPlain(out), nil
	}
	return out, nil
}

// mfjsOrdered deep-copies a schema into ordered objects: a map's keys are
// sorted (its JSON order), an ordered object keeps its order. Other values
// are kept as they are.
func mfjsOrdered(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		if x == nil {
			return v
		}
		o := jsonorder.FromMap(x)
		for _, k := range o.Keys() {
			child, _ := o.Get(k)
			o.Set(k, mfjsOrdered(child))
		}
		return o
	case *jsonorder.Object:
		if x == nil {
			return v
		}
		o := jsonorder.New()
		for _, k := range x.Keys() {
			child, _ := x.Get(k)
			o.Set(k, mfjsOrdered(child))
		}
		return o
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = mfjsOrdered(e)
		}
		return out
	}
	return v
}

// mfjsPlain turns the ordered objects of mfjsOrdered back into maps.
func mfjsPlain(v interface{}) interface{} {
	switch x := v.(type) {
	case *jsonorder.Object:
		m := make(map[string]interface{}, x.Len())
		for _, k := range x.Keys() {
			child, _ := x.Get(k)
			m[k] = mfjsPlain(child)
		}
		return m
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = mfjsPlain(e)
		}
		return out
	}
	return v
}

// normalizeMFJSDefinition is TS normalizeDefinition over a tree from
// mfjsOrdered (owned by the caller, so it is changed in place).
func normalizeMFJSDefinition(definition interface{}, isRoot bool) (interface{}, error) {
	result, ok := definition.(*jsonorder.Object)
	if !ok || result == nil {
		if isRoot {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: mfjsRootError,
			}
		}
		return definition, nil
	}

	// A root union of objects (Zod z.union / discriminatedUnion of objects) is
	// an object; MFJS only needs the root to say so ({ type: 'object', ...definition }).
	if _, hasType := result.Get("type"); isRoot && !hasType && isMFJSObjectUnion(result) {
		result = result.WithLead("type", "object")
	}

	if isRoot {
		if t, _ := result.Get("type"); t != "object" {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: mfjsRootError,
			}
		}
	}

	// Existing prefixItems stay first (they are positional); a tuple `items`
	// array is appended after them and converted to prefixItems.
	items, _ := result.Get("items")
	if tuple, ok := items.([]interface{}); ok {
		var prefix []interface{}
		if existing, ok := mfjsGet(result, "prefixItems").([]interface{}); ok {
			prefix = append(prefix, existing...)
		}
		for _, item := range tuple {
			normalized, err := normalizeMFJSDefinition(item, false)
			if err != nil {
				return nil, err
			}
			prefix = append(prefix, normalized)
		}
		if prefix == nil {
			prefix = []interface{}{}
		}
		result.Set("prefixItems", prefix)
		result.Delete("items")
	} else if itemsObj, ok := items.(*jsonorder.Object); ok {
		normalized, err := normalizeMFJSDefinition(itemsObj, false)
		if err != nil {
			return nil, err
		}
		result.Set("items", normalized)
	}

	// MFJS requires type to live inside anyOf items, except at the root, which
	// must keep type "object" (moving it leaves a typeless root that 400s).
	if parentType, ok := mfjsGet(result, "type").(string); ok && !isRoot {
		if anyOf, ok := mfjsGet(result, "anyOf").([]interface{}); ok {
			result.Delete("type")
			newAnyOf := make([]interface{}, len(anyOf))
			for i, branch := range anyOf {
				// TS: isRecord(branch) && branch.type == null ? { type: parentType, ...branch } : branch
				if b, ok := branch.(*jsonorder.Object); ok && b != nil && mfjsGet(b, "type") == nil {
					newAnyOf[i] = b.WithLead("type", parentType)
					continue
				}
				newAnyOf[i] = branch
			}
			result.Set("anyOf", newAnyOf)
		}
	}

	for _, key := range mfjsArrayKeys {
		arr, ok := mfjsGet(result, key).([]interface{})
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
		result.Set(key, newArr)
	}

	for _, key := range mfjsMapKeys {
		m, ok := mfjsGet(result, key).(*jsonorder.Object)
		if !ok || m == nil {
			continue
		}
		for _, k := range m.Keys() {
			child, _ := m.Get(k)
			normalized, err := normalizeMFJSDefinition(child, false)
			if err != nil {
				return nil, err
			}
			m.Set(k, normalized)
		}
	}

	for _, key := range mfjsSingleKeys {
		// Booleans pass through unchanged (normalizeDefinition returns the
		// value as-is for non-object, non-root definitions).
		if m, ok := mfjsGet(result, key).(*jsonorder.Object); ok && m != nil {
			normalized, err := normalizeMFJSDefinition(m, false)
			if err != nil {
				return nil, err
			}
			result.Set(key, normalized)
		}
	}

	return result, nil
}

func mfjsGet(o *jsonorder.Object, key string) interface{} {
	v, _ := o.Get(key)
	return v
}

// isMFJSObjectUnion reports whether every anyOf/oneOf on the root is a
// non-empty union of object schemas. Local $refs (#/$defs/X, #/definitions/X)
// are resolved one hop. A nullable branch (type: ["object", "null"]) is not an
// object, so such unions keep failing the root check.
func isMFJSObjectUnion(def *jsonorder.Object) bool {
	found := false
	for _, key := range []string{"anyOf", "oneOf"} {
		value, exists := def.Get(key)
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

func isMFJSObjectSchema(branch interface{}, root *jsonorder.Object) bool {
	resolved, ok := resolveMFJSLocalRef(branch, root).(*jsonorder.Object)
	if !ok || resolved == nil {
		return false
	}
	t, hasType := resolved.Get("type")
	if hasType {
		return t == "object"
	}
	_, isObj := mfjsGet(resolved, "properties").(*jsonorder.Object)
	return isObj
}

var mfjsLocalRef = regexp.MustCompile(`^#/(\$defs|definitions)/([^/]+)$`)

func resolveMFJSLocalRef(branch interface{}, root *jsonorder.Object) interface{} {
	b, ok := branch.(*jsonorder.Object)
	if !ok || b == nil {
		return branch
	}
	ref, ok := mfjsGet(b, "$ref").(string)
	if !ok {
		return branch
	}
	m := mfjsLocalRef.FindStringSubmatch(ref)
	if m == nil {
		return branch
	}
	if defs, ok := mfjsGet(root, m[1]).(*jsonorder.Object); ok && defs != nil {
		return mfjsGet(defs, m[2])
	}
	return branch
}
