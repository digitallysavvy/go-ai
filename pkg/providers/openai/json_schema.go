package openai

import (
	"regexp"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// NormalizeOpenAIJSONSchema is a Go port of TypeScript's
// normalizeOpenAIJsonSchema (normalize-openai-json-schema.ts, d5e3024 +
// 411b3f2). OpenAI's structured-outputs JSON Schema dialect does not support:
//
//   - the "propertyNames" keyword (removed recursively; a non-string
//     propertyNames sub-schema is rejected with UnsupportedFunctionalityError,
//     since it cannot be safely dropped without changing validation intent)
//   - "pattern" values containing regex lookaround ((?=...), (?!...),
//     (?<=...), (?<!...)) -- removed recursively, escape/character-class aware
//
// It operates on a generic map[string]interface{} JSON Schema representation
// (Go has no typed JSONSchema7 equivalent) and returns a new, normalized
// schema (the input is never mutated in place) plus compatibility warnings.
func NormalizeOpenAIJSONSchema(schema map[string]interface{}) (map[string]interface{}, []types.Warning, error) {
	var removedPropertyNames, removedLookaroundPattern bool

	normalized, err := normalizeJSONSchemaValueRoot(schema, &removedPropertyNames, &removedLookaroundPattern, true)
	if err != nil {
		return nil, nil, err
	}

	var warnings []types.Warning
	if removedPropertyNames {
		warnings = append(warnings, types.Warning{
			Type:    "compatibility",
			Feature: "JSON Schema propertyNames",
			Details: "OpenAI does not support JSON Schema propertyNames. It was removed before sending the schema, so OpenAI will not enforce property-name constraints.",
		})
	}
	if removedLookaroundPattern {
		warnings = append(warnings, types.Warning{
			Type:    "compatibility",
			Feature: "JSON Schema pattern with regex lookaround",
			Details: "OpenAI does not support regex lookaround in JSON Schema patterns. The pattern was removed before sending the schema, so OpenAI will not enforce that constraint.",
		})
	}

	normalizedMap, _ := normalized.(map[string]interface{})
	return normalizedMap, warnings, nil
}

// normalizeOpenAIChatToolSchemas normalizes the "parameters" schema of each
// function tool produced by tool.ToOpenAIFormat, mirroring TS
// prepareChatTools's normalizeOpenAIJsonSchema(tool.inputSchema) call
// (d5e3024, 411b3f2). Non-function tool entries are left untouched.
func normalizeOpenAIChatToolSchemas(tools []map[string]interface{}) ([]map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	for _, t := range tools {
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		params, ok := fn["parameters"].(map[string]interface{})
		if !ok || params == nil {
			continue
		}
		normalized, schemaWarnings, err := NormalizeOpenAIJSONSchema(params)
		if err != nil {
			return nil, nil, err
		}
		fn["parameters"] = normalized
		warnings = append(warnings, schemaWarnings...)
	}
	return tools, warnings, nil
}

// normalizeJSONSchemaValue normalizes a schema "definition", which per JSON
// Schema can be either a boolean (true/false, pass-through as-is) or an
// object schema. Nested definitions are never the document root, so a
// singleton-reference allOf wrapper here is always rewritten to a direct
// $ref (see normalizeJSONSchemaObject's isRoot parameter).
func normalizeJSONSchemaValue(def interface{}, removedPropertyNames, removedLookaroundPattern *bool) (interface{}, error) {
	return normalizeJSONSchemaValueRoot(def, removedPropertyNames, removedLookaroundPattern, false)
}

// normalizeJSONSchemaValueRoot is normalizeJSONSchemaValue with an explicit
// isRoot flag, used only by NormalizeOpenAIJSONSchema's initial call (isRoot
// = true): OpenAI requires an object at the schema root, so a root-level
// singleton reference must be expanded (inlined) rather than rewritten to a
// bare $ref.
func normalizeJSONSchemaValueRoot(def interface{}, removedPropertyNames, removedLookaroundPattern *bool, isRoot bool) (interface{}, error) {
	b, isBool := def.(bool)
	if isBool {
		return b, nil
	}
	m, ok := def.(map[string]interface{})
	if !ok || m == nil {
		return def, nil
	}
	return normalizeJSONSchemaObject(m, removedPropertyNames, removedLookaroundPattern, isRoot)
}

func normalizeJSONSchemaObject(schema map[string]interface{}, removedPropertyNames, removedLookaroundPattern *bool, isRoot bool) (map[string]interface{}, error) {
	if propertyNames, ok := schema["propertyNames"]; ok && propertyNames != nil {
		pnMap, isMap := propertyNames.(map[string]interface{})
		if !isMap || pnMap["type"] != "string" {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "JSON Schema propertyNames that does not use a string schema",
			}
		}
		*removedPropertyNames = true
	}

	// Shallow-copy so the caller's schema is never mutated.
	normalized := make(map[string]interface{}, len(schema))
	for k, v := range schema {
		normalized[k] = v
	}
	delete(normalized, "propertyNames")

	if pattern, ok := normalized["pattern"].(string); ok && containsRegexLookaround(pattern) {
		delete(normalized, "pattern")
		*removedLookaroundPattern = true
	}

	var err error
	if properties, ok := normalized["properties"].(map[string]interface{}); ok {
		if normalized["properties"], err = normalizeSchemaRecord(properties, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	if patternProperties, ok := normalized["patternProperties"].(map[string]interface{}); ok {
		if normalized["patternProperties"], err = normalizeSchemaRecord(patternProperties, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	if additionalProperties, ok := normalized["additionalProperties"]; ok && additionalProperties != nil {
		if normalized["additionalProperties"], err = normalizeJSONSchemaValue(additionalProperties, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	if additionalItems, ok := normalized["additionalItems"]; ok && additionalItems != nil {
		if normalized["additionalItems"], err = normalizeJSONSchemaValue(additionalItems, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	if items, ok := normalized["items"]; ok && items != nil {
		if itemsList, isList := items.([]interface{}); isList {
			out := make([]interface{}, len(itemsList))
			for i, item := range itemsList {
				if out[i], err = normalizeJSONSchemaValue(item, removedPropertyNames, removedLookaroundPattern); err != nil {
					return nil, err
				}
			}
			normalized["items"] = out
		} else {
			if normalized["items"], err = normalizeJSONSchemaValue(items, removedPropertyNames, removedLookaroundPattern); err != nil {
				return nil, err
			}
		}
	}
	if contains, ok := normalized["contains"]; ok && contains != nil {
		if normalized["contains"], err = normalizeJSONSchemaValue(contains, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	if not, ok := normalized["not"]; ok && not != nil {
		if normalized["not"], err = normalizeJSONSchemaValue(not, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if list, ok := normalized[key].([]interface{}); ok {
			out := make([]interface{}, len(list))
			for i, item := range list {
				if out[i], err = normalizeJSONSchemaValue(item, removedPropertyNames, removedLookaroundPattern); err != nil {
					return nil, err
				}
			}
			normalized[key] = out
		}
	}
	if definitions, ok := normalized["definitions"].(map[string]interface{}); ok {
		if normalized["definitions"], err = normalizeSchemaRecord(definitions, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	if defs, ok := normalized["$defs"].(map[string]interface{}); ok {
		if normalized["$defs"], err = normalizeSchemaRecord(defs, removedPropertyNames, removedLookaroundPattern); err != nil {
			return nil, err
		}
	}
	if dependencies, ok := normalized["dependencies"].(map[string]interface{}); ok {
		out := make(map[string]interface{}, len(dependencies))
		for k, v := range dependencies {
			if _, isList := v.([]interface{}); isList {
				out[k] = v
			} else {
				if out[k], err = normalizeJSONSchemaValue(v, removedPropertyNames, removedLookaroundPattern); err != nil {
					return nil, err
				}
			}
		}
		normalized["dependencies"] = out
	}
	for _, keyword := range []string{"if", "then", "else"} {
		if cond, ok := normalized[keyword]; ok && cond != nil {
			if normalized[keyword], err = normalizeJSONSchemaValue(cond, removedPropertyNames, removedLookaroundPattern); err != nil {
				return nil, err
			}
		}
	}

	// Zod 4 represents recursive references as singleton `allOf` schemas
	// ({"allOf": [{"$ref": "..."}]}). OpenAI does not support `allOf`, but a
	// singleton local reference validates identically to a direct $ref, so
	// rewrite it (TS commit 4e94782655, #21678).
	reference, hasReference := getSingletonReference(normalized)
	if !hasReference {
		return normalized, nil
	}

	schemaWithoutAllOf := make(map[string]interface{}, len(normalized))
	for k, v := range normalized {
		if k != "allOf" {
			schemaWithoutAllOf[k] = v
		}
	}

	if !isRoot {
		// A one-item allOf has the same validation behavior as its reference.
		schemaWithoutAllOf["$ref"] = reference
		return schemaWithoutAllOf, nil
	}

	// OpenAI requires an object at the schema root, so a local root
	// reference must be expanded instead of being sent as a direct $ref.
	referencedSchema, found := getLocalReferenceSchema(reference, normalized)
	if !found {
		return normalized, nil
	}

	merged := make(map[string]interface{}, len(referencedSchema)+len(schemaWithoutAllOf))
	for k, v := range referencedSchema {
		merged[k] = v
	}
	for k, v := range schemaWithoutAllOf {
		merged[k] = v
	}
	return merged, nil
}

// getSingletonReference reports whether schema's "allOf" is a one-item list
// whose sole entry is a bare {"$ref": "..."} object (i.e. an intersection
// that is equivalent to a direct reference). Any other allOf shape -- more
// than one entry, or an entry with additional keys alongside $ref -- is left
// untouched, matching TS's getSingletonReference.
func getSingletonReference(schema map[string]interface{}) (string, bool) {
	allOf, ok := schema["allOf"].([]interface{})
	if !ok || len(allOf) != 1 {
		return "", false
	}
	entry, ok := allOf[0].(map[string]interface{})
	if !ok || len(entry) != 1 {
		return "", false
	}
	ref, ok := entry["$ref"].(string)
	if !ok {
		return "", false
	}
	return ref, true
}

var localJSONSchemaReferenceRe = regexp.MustCompile(`^#/(definitions|\$defs)/(.+)$`)

// getLocalReferenceSchema resolves a "#/definitions/<name>" or
// "#/$defs/<name>" reference against schema's own definitions/$defs,
// unescaping the JSON Pointer "~1" ("/") and "~0" ("~") sequences in name.
func getLocalReferenceSchema(reference string, schema map[string]interface{}) (map[string]interface{}, bool) {
	match := localJSONSchemaReferenceRe.FindStringSubmatch(reference)
	if match == nil {
		return nil, false
	}
	keyword, encodedName := match[1], match[2]
	name := strings.ReplaceAll(strings.ReplaceAll(encodedName, "~1", "/"), "~0", "~")

	var definitions map[string]interface{}
	if keyword == "definitions" {
		definitions, _ = schema["definitions"].(map[string]interface{})
	} else {
		definitions, _ = schema["$defs"].(map[string]interface{})
	}
	definition, ok := definitions[name].(map[string]interface{})
	if !ok {
		return nil, false
	}
	return definition, true
}

func normalizeSchemaRecord(schemas map[string]interface{}, removedPropertyNames, removedLookaroundPattern *bool) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(schemas))
	for k, v := range schemas {
		normalized, err := normalizeJSONSchemaValue(v, removedPropertyNames, removedLookaroundPattern)
		if err != nil {
			return nil, err
		}
		out[k] = normalized
	}
	return out, nil
}

// containsRegexLookaround is a Go port of TS's escape/character-class-aware
// containsRegexLookaround, detecting (?=...), (?!...), (?<=...), (?<!...).
func containsRegexLookaround(pattern string) bool {
	escaped := false
	inCharacterClass := false

	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		c := runes[i]

		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '[' {
			inCharacterClass = true
			continue
		}
		if c == ']' {
			inCharacterClass = false
			continue
		}
		if !inCharacterClass && c == '(' && i+1 < len(runes) && runes[i+1] == '?' {
			var lookaroundPrefix rune
			if i+2 < len(runes) {
				lookaroundPrefix = runes[i+2]
			}
			if lookaroundPrefix == '=' || lookaroundPrefix == '!' {
				return true
			}
			if lookaroundPrefix == '<' && i+3 < len(runes) && (runes[i+3] == '=' || runes[i+3] == '!') {
				return true
			}
		}
	}

	return false
}
