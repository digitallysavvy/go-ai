package anthropic

import "strings"

// maxNativeStructuredOutputOptionalProperties mirrors TS
// MAX_OPTIONAL_PROPERTIES: Anthropic rejects schemas with more than 24
// optional properties in total, and grammar compilation fails earlier as
// optional properties add up (from 13 on Bedrock and the Anthropic API, from
// 11 on Vertex).
const maxNativeStructuredOutputOptionalProperties = 10

var (
	nativeSOSubschemaKeywords     = []string{"additionalProperties", "contains", "else", "if", "items", "not", "then"}
	nativeSOSubschemaListKeywords = []string{"allOf", "anyOf", "oneOf", "prefixItems"}
	nativeSOSubschemaMapKeywords  = []string{"dependentSchemas", "patternProperties", "properties"}
)

type nativeSOSchemaStats struct {
	optionalProperties int
	hasOpenObject      bool
}

// SupportsNativeStructuredOutputSchema ports TS
// supportsNativeStructuredOutputSchema (TS b0d51fecc0): whether Anthropic's
// native structured outputs (output_config.format) can represent schema
// without rejecting it or losing data.
//
// Rejected: self-referencing schemas and schemas with more than 10 optional
// properties (counted at every $ref use). Lossy: open objects (maps,
// free-form objects), which native structured outputs close with
// additionalProperties: false, so the model could only return {} for them.
//
// schema may be any form a ResponseFormat.Schema takes (a map,
// *schema.OrderedSchema, a schema.Schema). Bedrock uses it for its Anthropic
// models.
func SupportsNativeStructuredOutputSchema(schema interface{}) bool {
	root, ok := genericSchema(schema)
	if !ok {
		return true
	}
	if hasCircularSchemaReference(root) {
		return false
	}
	stats := nativeSOStats(root, root, map[string]nativeSOSchemaStats{})
	return !stats.hasOpenObject && stats.optionalProperties <= maxNativeStructuredOutputOptionalProperties
}

func nativeSOStats(node, root interface{}, refStats map[string]nativeSOSchemaStats) nativeSOSchemaStats {
	stats := nativeSOSchemaStats{}
	n, ok := node.(map[string]interface{})
	if !ok {
		return stats
	}
	add := func(child interface{}) {
		c := nativeSOStats(child, root, refStats)
		stats.optionalProperties += c.optionalProperties
		stats.hasOpenObject = stats.hasOpenObject || c.hasOpenObject
	}

	// References are counted at every use; the schema has no cycles here.
	if ref, ok := n["$ref"].(string); ok && strings.HasPrefix(ref, "#") {
		resolved, seen := refStats[ref]
		if !seen {
			target, _ := resolveSchemaPointer(root, ref)
			resolved = nativeSOStats(target, root, refStats)
			refStats[ref] = resolved
		}
		stats.optionalProperties += resolved.optionalProperties
		stats.hasOpenObject = stats.hasOpenObject || resolved.hasOpenObject
	}

	if isOpenObjectSchema(n) {
		stats.hasOpenObject = true
	}

	if props, ok := n["properties"].(map[string]interface{}); ok {
		required := map[string]bool{}
		if req, ok := n["required"].([]interface{}); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					required[s] = true
				}
			}
		}
		for name := range props {
			if !required[name] {
				stats.optionalProperties++
			}
		}
	}

	for _, keyword := range nativeSOSubschemaKeywords {
		if list, ok := n[keyword].([]interface{}); ok {
			for _, item := range list {
				add(item)
			}
		} else {
			add(n[keyword])
		}
	}
	for _, keyword := range nativeSOSubschemaListKeywords {
		if list, ok := n[keyword].([]interface{}); ok {
			for _, item := range list {
				add(item)
			}
		}
	}
	for _, keyword := range nativeSOSubschemaMapKeywords {
		if m, ok := n[keyword].(map[string]interface{}); ok {
			for _, sub := range m {
				add(sub)
			}
		}
	}
	return stats
}

// isOpenObjectSchema ports TS isOpenObject.
func isOpenObjectSchema(n map[string]interface{}) bool {
	additional, hasAdditional := n["additionalProperties"]
	if b, ok := additional.(bool); ok && b {
		return true
	}
	if _, ok := additional.(map[string]interface{}); ok {
		return true
	}
	if pp, ok := n["patternProperties"].(map[string]interface{}); ok && len(pp) > 0 {
		return true
	}
	isObjectType := false
	switch t := n["type"].(type) {
	case string:
		isObjectType = t == "object"
	case []interface{}:
		for _, v := range t {
			if v == "object" {
				isObjectType = true
			}
		}
	}
	closed := false
	if b, ok := additional.(bool); ok && hasAdditional && !b {
		closed = true
	}
	return isObjectType && n["properties"] == nil && !closed
}
