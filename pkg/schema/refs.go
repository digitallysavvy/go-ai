package schema

import (
	"fmt"
	"strings"
)

// resolveLocalRef resolves a single JSON Pointer of the form
// "#/$defs/Name" or "#/definitions/Name/nested/path" against root. Only
// local, in-document references are supported -- this mirrors the
// $defs/definitions hoisting pkg/ai performs when building tool/output
// schemas (see pkg/ai/output.go's ResponseFormat and pkg/ai/object.go's
// hoistSchemaDefs, both of which hoist $defs to the document root so
// "#/$defs/..." refs resolve correctly).
//
// A ref that does not start with "#/" (an external/remote reference) is not
// an error: found is false and validation for that node is skipped, since
// go-ai has no document to fetch it from and treating it as unconstrained is
// safer than hard-failing a schema we cannot fully understand.
func resolveLocalRef(root map[string]interface{}, ref string) (resolved map[string]interface{}, found bool, err error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false, nil
	}
	pointer := strings.TrimPrefix(ref, "#/")
	if root == nil {
		return nil, false, fmt.Errorf("%q: no document root to resolve against", ref)
	}

	var current interface{} = root
	if pointer != "" {
		for _, rawSegment := range strings.Split(pointer, "/") {
			segment := unescapeJSONPointerSegment(rawSegment)
			m, ok := current.(map[string]interface{})
			if !ok {
				return nil, false, fmt.Errorf("%q: %q is not an object", ref, segment)
			}
			next, exists := m[segment]
			if !exists {
				return nil, false, fmt.Errorf("%q: %q not found", ref, segment)
			}
			current = next
		}
	}

	m, ok := current.(map[string]interface{})
	if !ok {
		return nil, false, fmt.Errorf("%q does not resolve to an object schema", ref)
	}
	return m, true, nil
}

// resolveRefChain resolves ref against root, following any further $ref
// found in the resolved schema until it reaches a ref-free schema. It
// returns (nil, nil) when the initial ref is external/unresolvable (see
// resolveLocalRef), and an error only for a malformed local pointer or a
// circular/too-deep $ref chain.
func resolveRefChain(root map[string]interface{}, ref string) (map[string]interface{}, error) {
	visited := make(map[string]bool)
	current := ref
	for {
		if visited[current] {
			return nil, fmt.Errorf("circular $ref chain at %q", current)
		}
		if len(visited) > 100 {
			return nil, fmt.Errorf("$ref chain exceeds maximum depth")
		}
		visited[current] = true

		resolved, found, err := resolveLocalRef(root, current)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		nextRef, hasNext := resolved["$ref"].(string)
		if !hasNext || nextRef == "" {
			return resolved, nil
		}
		current = nextRef
	}
}

// schemaDefault returns the effective "default" value for sch, following a
// bare "$ref" schema (one with no sibling "default") to the referenced
// schema's own default. This lets a property declared as `{"$ref":
// "#/$defs/Region"}` pick up a default declared on the $defs.Region schema
// itself.
func schemaDefault(sch map[string]interface{}, root map[string]interface{}) (interface{}, bool) {
	if def, ok := sch["default"]; ok {
		return def, true
	}
	if refVal, ok := sch["$ref"].(string); ok && refVal != "" {
		if resolved, err := resolveRefChain(root, refVal); err == nil && resolved != nil {
			return schemaDefault(resolved, root)
		}
	}
	return nil, false
}

// unescapeJSONPointerSegment decodes the RFC 6901 escapes used inside a JSON
// Pointer segment: "~1" for "/" and "~0" for "~".
func unescapeJSONPointerSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "~1", "/")
	segment = strings.ReplaceAll(segment, "~0", "~")
	return segment
}
