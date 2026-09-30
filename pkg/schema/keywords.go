package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sync"
	"unicode/utf8"
)

// regexCache memoizes compiled "pattern"/"patternProperties" regular
// expressions. Tool schemas are typically re-wrapped (schema.NewJSONSchema)
// on every tool call, so without a cache the same pattern would be
// recompiled on every validation.
var regexCache sync.Map // map[string]*regexp.Regexp

// compileRegex compiles pattern using Go's RE2 engine (regexp package).
//
// Note: RE2 is not identical to the ECMA-262 regular expression dialect that
// JSON Schema (and TypeScript/zod) assumes. Most common patterns (character
// classes, anchors, quantifiers) behave the same, but RE2 does not support
// backreferences (e.g. `\1`) or lookaround (`(?=...)`, `(?!...)`,
// `(?<=...)`, `(?<!...)`). A schema that relies on those constructs will
// fail to compile here; compileRegex reports that as a validation error
// naming the pattern rather than panicking.
func compileRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := regexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, re)
	return re, nil
}

// validateStringConstraints applies minLength/maxLength/pattern/format to a
// string value. Lengths are measured in Unicode code points, matching JSON
// Schema's definition of string length (not UTF-16 code units, and not
// bytes).
func validateStringConstraints(s string, sch map[string]interface{}, path string) error {
	length := utf8.RuneCountInString(s)
	if minLen, ok := toInt(sch["minLength"]); ok && length < minLen {
		return fmt.Errorf("%s: minLength: string length %d is less than %d", path, length, minLen)
	}
	if maxLen, ok := toInt(sch["maxLength"]); ok && length > maxLen {
		return fmt.Errorf("%s: maxLength: string length %d is greater than %d", path, length, maxLen)
	}
	if patternRaw, ok := sch["pattern"].(string); ok && patternRaw != "" {
		re, err := compileRegex(patternRaw)
		if err != nil {
			return fmt.Errorf("%s: pattern: invalid regular expression %q: %s", path, patternRaw, err.Error())
		}
		if !re.MatchString(s) {
			return fmt.Errorf("%s: pattern: string does not match pattern %q", path, patternRaw)
		}
	}
	if formatRaw, ok := sch["format"].(string); ok && formatRaw != "" {
		if err := validateFormat(s, formatRaw); err != nil {
			return fmt.Errorf("%s: format: %s", path, err.Error())
		}
	}
	return nil
}

// validateNumberConstraints applies minimum/maximum/exclusiveMinimum/
// exclusiveMaximum/multipleOf. exclusiveMinimum/exclusiveMaximum support both
// the draft-07 boolean form (paired with minimum/maximum) and the draft
// 2020-12 numeric form.
func validateNumberConstraints(value interface{}, sch map[string]interface{}, path string) error {
	n, ok := toFloat64(value)
	if !ok {
		return nil
	}

	if minRaw, ok := toFloat64(sch["minimum"]); ok && n < minRaw {
		return fmt.Errorf("%s: minimum: value %v is less than %v", path, n, minRaw)
	}
	if maxRaw, ok := toFloat64(sch["maximum"]); ok && n > maxRaw {
		return fmt.Errorf("%s: maximum: value %v is greater than %v", path, n, maxRaw)
	}

	switch exMin := sch["exclusiveMinimum"].(type) {
	case bool:
		if exMin {
			if minRaw, ok := toFloat64(sch["minimum"]); ok && n <= minRaw {
				return fmt.Errorf("%s: exclusiveMinimum: value %v must be greater than %v", path, n, minRaw)
			}
		}
	default:
		if exMinRaw, ok := toFloat64(sch["exclusiveMinimum"]); ok && n <= exMinRaw {
			return fmt.Errorf("%s: exclusiveMinimum: value %v must be greater than %v", path, n, exMinRaw)
		}
	}

	switch exMax := sch["exclusiveMaximum"].(type) {
	case bool:
		if exMax {
			if maxRaw, ok := toFloat64(sch["maximum"]); ok && n >= maxRaw {
				return fmt.Errorf("%s: exclusiveMaximum: value %v must be less than %v", path, n, maxRaw)
			}
		}
	default:
		if exMaxRaw, ok := toFloat64(sch["exclusiveMaximum"]); ok && n >= exMaxRaw {
			return fmt.Errorf("%s: exclusiveMaximum: value %v must be less than %v", path, n, exMaxRaw)
		}
	}

	if multRaw, ok := toFloat64(sch["multipleOf"]); ok && multRaw != 0 {
		quotient := n / multRaw
		if math.Abs(quotient-math.Round(quotient)) > 1e-9 {
			return fmt.Errorf("%s: multipleOf: value %v is not a multiple of %v", path, n, multRaw)
		}
	}

	return nil
}

// validateArrayConstraints applies minItems/maxItems/uniqueItems plus
// per-element validation: 2020-12 "prefixItems" (positional schemas, with
// "items" constraining the remainder) or draft-07 tuple-form "items" (an
// array of schemas, with "additionalItems" constraining the remainder). A
// single-schema "items" (the pre-existing, common case) still applies to
// every element.
func validateArrayConstraints(value interface{}, sch map[string]interface{}, path string, root map[string]interface{}) error {
	rv := reflect.ValueOf(value)
	n := rv.Len()

	if minItems, ok := toInt(sch["minItems"]); ok && n < minItems {
		return fmt.Errorf("%s: minItems: array has %d items, fewer than %d", path, n, minItems)
	}
	if maxItems, ok := toInt(sch["maxItems"]); ok && n > maxItems {
		return fmt.Errorf("%s: maxItems: array has %d items, more than %d", path, n, maxItems)
	}
	if unique, ok := sch["uniqueItems"].(bool); ok && unique {
		if err := validateUniqueItems(rv, n, path); err != nil {
			return err
		}
	}

	if prefixItems, ok := interfaceSlice(sch["prefixItems"]); ok && len(prefixItems) > 0 {
		return validateTupleItems(rv, n, prefixItems, sch["items"], nil, path, root)
	}

	if tupleItems, ok := interfaceSlice(sch["items"]); ok {
		return validateTupleItems(rv, n, tupleItems, nil, sch["additionalItems"], path, root)
	}

	if items, ok := sch["items"].(map[string]interface{}); ok {
		for i := 0; i < n; i++ {
			if err := validateSchemaValue(rv.Index(i).Interface(), items, fmt.Sprintf("%s[%d]", path, i), root, newRefGuard()); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateUniqueItems checks the JSON Schema "uniqueItems" keyword, which
// requires deep equality between elements (per spec, JSON numbers compare by
// mathematical value regardless of their representation -- see
// jsonValuesEqual). A naive pairwise comparison is O(n^2) and becomes a
// bottleneck on large arrays (e.g. a tool argument with thousands of
// elements), so elements are hashed into a canonical JSON encoding instead:
// encoding/json sorts object keys and normalizes numeric formatting
// (float64(1) and int(1) both marshal to "1"), so two deep-equal elements
// always produce the same key, giving amortized O(n) duplicate detection.
// Elements that fail to marshal (a decoded JSON value never does, but a
// hand-built schema.Schema Go value in principle could contain something
// exotic like a function) fall back to an O(n^2) DeepEqual-based scan so
// correctness never depends on marshaling success.
func validateUniqueItems(rv reflect.Value, n int, path string) error {
	seenKeys := make(map[string]int, n)
	var fallback []interface{}
	for i := 0; i < n; i++ {
		elem := rv.Index(i).Interface()
		key, err := canonicalUniqueItemsKey(elem)
		if err != nil {
			for _, prior := range fallback {
				if jsonValuesEqual(elem, prior) {
					return fmt.Errorf("%s: uniqueItems: duplicate item at index %d", path, i)
				}
			}
			fallback = append(fallback, elem)
			continue
		}
		if _, dup := seenKeys[key]; dup {
			return fmt.Errorf("%s: uniqueItems: duplicate item at index %d", path, i)
		}
		seenKeys[key] = i
	}
	return nil
}

// canonicalUniqueItemsKey encodes value as canonical JSON for use as a
// uniqueItems dedup key. All JSON-decodable numeric Go types are normalized
// to float64 first so that, e.g., int(1) and float64(1) (which JSON Schema
// treats as the equal number 1) produce the same key.
func canonicalUniqueItemsKey(value interface{}) (string, error) {
	normalized, err := normalizeForCanonicalKey(value)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func normalizeForCanonicalKey(value interface{}) (interface{}, error) {
	if f, ok := toFloat64(value); ok {
		return f, nil
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, nil
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]interface{}, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			normalized, err := normalizeForCanonicalKey(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("unsupported map key type %s", rv.Type().Key())
		}
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			normalized, err := normalizeForCanonicalKey(iter.Value().Interface())
			if err != nil {
				return nil, err
			}
			out[iter.Key().String()] = normalized
		}
		return out, nil
	default:
		return value, nil
	}
}

// validateTupleItems validates the positional tuple schemas against the
// first len(tupleItems) elements, then validates any remaining elements
// against restItems (2020-12 "items" after "prefixItems") or additionalItems
// (draft-07: a schema, or `false` to forbid extra elements).
func validateTupleItems(rv reflect.Value, n int, tupleItems []interface{}, restItemsRaw interface{}, additionalItemsRaw interface{}, path string, root map[string]interface{}) error {
	for i := 0; i < n && i < len(tupleItems); i++ {
		sub, ok := tupleItems[i].(map[string]interface{})
		if !ok {
			continue
		}
		if err := validateSchemaValue(rv.Index(i).Interface(), sub, fmt.Sprintf("%s[%d]", path, i), root, newRefGuard()); err != nil {
			return err
		}
	}

	if n <= len(tupleItems) {
		return nil
	}

	if restSchema, ok := restItemsRaw.(map[string]interface{}); ok {
		for i := len(tupleItems); i < n; i++ {
			if err := validateSchemaValue(rv.Index(i).Interface(), restSchema, fmt.Sprintf("%s[%d]", path, i), root, newRefGuard()); err != nil {
				return err
			}
		}
		return nil
	}

	switch additional := additionalItemsRaw.(type) {
	case bool:
		if !additional {
			return fmt.Errorf("%s: items: array must not contain more than %d items", path, len(tupleItems))
		}
	case map[string]interface{}:
		for i := len(tupleItems); i < n; i++ {
			if err := validateSchemaValue(rv.Index(i).Interface(), additional, fmt.Sprintf("%s[%d]", path, i), root, newRefGuard()); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateObjectConstraints applies minProperties/maxProperties/
// patternProperties/propertyNames/additionalProperties (bool or schema-valued)
// to an object value. "properties" itself is validated by the caller
// (validateSchemaValue), which also needs the raw obj/props for its own
// error path; this function receives the already-decoded obj map.
func validateObjectConstraints(obj map[string]interface{}, sch map[string]interface{}, path string, root map[string]interface{}) error {
	if minProps, ok := toInt(sch["minProperties"]); ok && len(obj) < minProps {
		return fmt.Errorf("%s: minProperties: object has %d properties, fewer than %d", path, len(obj), minProps)
	}
	if maxProps, ok := toInt(sch["maxProperties"]); ok && len(obj) > maxProps {
		return fmt.Errorf("%s: maxProperties: object has %d properties, more than %d", path, len(obj), maxProps)
	}

	if propertyNamesSchema, ok := sch["propertyNames"].(map[string]interface{}); ok {
		for key := range obj {
			if err := validateSchemaValue(key, propertyNamesSchema, fmt.Sprintf("%s.%s (property name)", path, key), root, newRefGuard()); err != nil {
				return err
			}
		}
	}

	patternProps, _ := sch["patternProperties"].(map[string]interface{})
	type compiledPatternSchema struct {
		re     *regexp.Regexp
		schema map[string]interface{}
	}
	var compiled []compiledPatternSchema
	for pattern, rawSubschema := range patternProps {
		sub, ok := rawSubschema.(map[string]interface{})
		if !ok {
			continue
		}
		re, err := compileRegex(pattern)
		if err != nil {
			return fmt.Errorf("%s: patternProperties: invalid regular expression %q: %s", path, pattern, err.Error())
		}
		compiled = append(compiled, compiledPatternSchema{re: re, schema: sub})
	}
	matchesPattern := func(key string) bool {
		for _, cp := range compiled {
			if cp.re.MatchString(key) {
				return true
			}
		}
		return false
	}
	for _, cp := range compiled {
		for key, val := range obj {
			if cp.re.MatchString(key) {
				if err := validateSchemaValue(val, cp.schema, path+"."+key, root, newRefGuard()); err != nil {
					return err
				}
			}
		}
	}

	props, _ := sch["properties"].(map[string]interface{})
	switch additional := sch["additionalProperties"].(type) {
	case bool:
		if !additional {
			for key := range obj {
				if _, declared := props[key]; declared {
					continue
				}
				if matchesPattern(key) {
					continue
				}
				return fmt.Errorf("%s.%s: additionalProperties: property is not allowed", path, key)
			}
		}
	case map[string]interface{}:
		for key, val := range obj {
			if _, declared := props[key]; declared {
				continue
			}
			if matchesPattern(key) {
				continue
			}
			if err := validateSchemaValue(val, additional, path+"."+key, root, newRefGuard()); err != nil {
				return err
			}
		}
	}

	return nil
}
