package anthropic

import (
	"encoding/json"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// schemaMapKeywords are the keywords whose values map names to subschemas
// rather than holding a schema (TS has-circular-schema-reference.ts
// SCHEMA_MAP_KEYWORDS).
var schemaMapKeywords = map[string]bool{
	"dependentSchemas":  true,
	"patternProperties": true,
	"properties":        true,
}

// schemaDataKeywords hold instance data, which can contain a literal "$ref"
// key (TS DATA_KEYWORDS).
var schemaDataKeywords = map[string]bool{
	"const":    true,
	"default":  true,
	"enum":     true,
	"examples": true,
}

// genericSchema returns schema as plain JSON values (map[string]interface{},
// []interface{}, string, float64, bool, nil), whatever Go form the caller
// used (a map, *schema.OrderedSchema, a schema.Schema). ok is false when the
// schema does not encode to JSON.
func genericSchema(schema interface{}) (interface{}, bool) {
	raw, err := json.Marshal(providerutils.ResponseFormatJSONSchemaOrdered(schema))
	if err != nil {
		return nil, false
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

// hasCircularSchemaReference ports TS hasCircularSchemaReference (TS
// f4c5f1e959): whether a JSON schema contains a cycle of local $refs, such as
// a tree node whose children reference the node itself. Anthropic's native
// structured outputs reject such schemas; the JSON response tool accepts
// them.
func hasCircularSchemaReference(root interface{}) bool {
	visiting := map[string]bool{}
	visited := map[string]bool{}

	var visit func(pointer string) bool
	visit = func(pointer string) bool {
		if visiting[pointer] {
			return true
		}
		if visited[pointer] {
			return false
		}
		target, ok := resolveSchemaPointer(root, pointer)
		if !ok {
			return false
		}
		visiting[pointer] = true
		circular := false
		for _, ref := range collectLocalRefs(target, nil) {
			if visit(ref) {
				circular = true
				break
			}
		}
		delete(visiting, pointer)
		visited[pointer] = true
		return circular
	}

	return visit("#")
}

func collectLocalRefs(node interface{}, refs []string) []string {
	switch n := node.(type) {
	case []interface{}:
		for _, item := range n {
			refs = collectLocalRefs(item, refs)
		}
	case map[string]interface{}:
		for key, value := range n {
			m, isRecord := value.(map[string]interface{})
			switch {
			case key == "$ref":
				if s, ok := value.(string); ok && strings.HasPrefix(s, "#") {
					refs = append(refs, s)
				}
			case schemaMapKeywords[key] && isRecord:
				for _, sub := range m {
					refs = collectLocalRefs(sub, refs)
				}
			case key != "$defs" && key != "definitions" && !schemaDataKeywords[key]:
				// definitions are only reached through a $ref.
				refs = collectLocalRefs(value, refs)
			}
		}
	}
	return refs
}

// resolveSchemaPointer ports TS resolveSchemaPointer: it resolves a local
// JSON pointer ("#", "#/definitions/Node") against root. ok is false when the
// pointer is not local, does not decode, or names nothing (TS undefined).
func resolveSchemaPointer(root interface{}, pointer string) (interface{}, bool) {
	if pointer == "#" {
		return root, true
	}
	if !strings.HasPrefix(pointer, "#/") {
		return nil, false
	}
	// TS decodeURIComponent throws on a malformed escape or invalid UTF-8.
	decoded, err := url.PathUnescape(pointer[2:])
	if err != nil || !utf8.ValidString(decoded) {
		return nil, false
	}

	current := root
	for _, raw := range strings.Split(decoded, "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch c := current.(type) {
		case []interface{}:
			// current[Number(segment)]: only an integral in-range index names
			// an element.
			f, ok := providerutils.JSNumber(segment)
			if !ok || f != math.Trunc(f) || f < 0 || f >= float64(len(c)) {
				return nil, false
			}
			current = c[int(f)]
		case map[string]interface{}:
			v, ok := c[segment]
			if !ok {
				return nil, false
			}
			current = v
		default:
			return nil, false
		}
	}
	return current, true
}
