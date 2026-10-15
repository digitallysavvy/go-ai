// Package zodlite is a minimal port of the zod validation semantics that the
// TS providers apply to parsed streaming frames (anthropicChunkSchema,
// google chunkSchema). zod objects are non-strict (unknown keys pass),
// `nullish` means absent or null, `optional` means absent only, and
// `nullable` means the key must be present but may be null. Only the first
// failing issue is reported.
package zodlite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Issue is one validation failure.
type Issue struct {
	Path    []interface{}
	Code    string
	Message string
}

// Node validates one value; present is false when the object key is absent.
type Node func(v interface{}, present bool, path []interface{}) *Issue

// JSType names the JS type of a decoded JSON value.
func JSType(v interface{}, present bool) string {
	switch v.(type) {
	case nil:
		if !present {
			return "undefined"
		}
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	}
	return "unknown"
}

// Invalid builds the zod v4 invalid_type issue.
func Invalid(expected string, v interface{}, present bool, path []interface{}) *Issue {
	return &Issue{Path: path, Code: "invalid_type", Message: "Invalid input: expected " + expected + ", received " + JSType(v, present)}
}

// String is z.string().
func String(v interface{}, present bool, path []interface{}) *Issue {
	if _, ok := v.(string); !ok {
		return Invalid("string", v, present, path)
	}
	return nil
}

// Number is z.number().
func Number(v interface{}, present bool, path []interface{}) *Issue {
	if _, ok := v.(float64); !ok {
		return Invalid("number", v, present, path)
	}
	return nil
}

// Boolean is z.boolean().
func Boolean(v interface{}, present bool, path []interface{}) *Issue {
	if _, ok := v.(bool); !ok {
		return Invalid("boolean", v, present, path)
	}
	return nil
}

// Unknown is z.unknown() / z.json(): any value passes.
func Unknown(interface{}, bool, []interface{}) *Issue { return nil }

// Literal is z.literal(want) for strings.
func Literal(want string) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		if s, ok := v.(string); ok && s == want {
			return nil
		}
		return &Issue{Path: path, Code: "invalid_value", Message: fmt.Sprintf("Invalid input: expected %q", want)}
	}
}

// Nullish accepts an absent key or null.
func Nullish(n Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		if !present || v == nil {
			return nil
		}
		return n(v, present, path)
	}
}

// Nullable accepts null when the key is present.
func Nullable(n Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		if present && v == nil {
			return nil
		}
		return n(v, present, path)
	}
}

// Optional accepts an absent key.
func Optional(n Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		if !present {
			return nil
		}
		return n(v, present, path)
	}
}

// Array is z.array(n).
func Array(n Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		a, ok := v.([]interface{})
		if !ok {
			return Invalid("array", v, present, path)
		}
		for i, e := range a {
			if bad := n(e, true, append(append([]interface{}{}, path...), i)); bad != nil {
				return bad
			}
		}
		return nil
	}
}

// Field is one key of an object schema.
type Field struct {
	Key  string
	Node Node
}

// F builds a Field.
func F(key string, n Node) Field { return Field{key, n} }

// Object is a non-strict z.object.
func Object(fields ...Field) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return Invalid("object", v, present, path)
		}
		for _, f := range fields {
			fv, fp := m[f.Key]
			if bad := f.Node(fv, fp, append(append([]interface{}{}, path...), f.Key)); bad != nil {
				return bad
			}
		}
		return nil
	}
}

// Record is z.record(z.string(), z.unknown()): an object with any values.
func Record(v interface{}, present bool, path []interface{}) *Issue {
	if _, ok := v.(map[string]interface{}); !ok {
		return Invalid("record", v, present, path)
	}
	return nil
}

// Union is z.union: the first matching node wins.
func Union(nodes ...Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		for _, n := range nodes {
			if n(v, present, path) == nil {
				return nil
			}
		}
		return &Issue{Path: path, Code: "invalid_union", Message: "Invalid input"}
	}
}

// Disc is z.discriminatedUnion('type', ...).
func Disc(variants map[string]Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return Invalid("object", v, present, path)
		}
		t, _ := m["type"].(string)
		n, known := variants[t]
		if !known {
			return &Issue{Path: append(append([]interface{}{}, path...), "type"), Code: "invalid_union", Message: "Invalid input"}
		}
		return n(v, present, path)
	}
}

// Typed is an object whose `type` literal is t plus the extra fields.
func Typed(t string, fields ...Field) Node {
	return Object(append([]Field{{"type", Literal(t)}}, fields...)...)
}

// ValidationMessage validates a JSON frame against schema. It returns ""
// when the frame matches (or is not JSON, which callers report separately),
// otherwise the TS TypeValidationError message. The zod issue text is a
// best-effort rendering (path plus the zod v4 message of the first failing
// field).
func ValidationMessage(schema Node, data string) string {
	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		return ""
	}
	bad := schema(value, true, nil)
	if bad == nil {
		return ""
	}
	path := bad.Path
	if path == nil {
		path = []interface{}{}
	}
	var issues bytes.Buffer
	enc := json.NewEncoder(&issues)
	enc.SetEscapeHTML(false)
	_ = enc.Encode([]map[string]interface{}{{"code": bad.Code, "path": path, "message": bad.Message}})
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(data)); err != nil {
		compact.WriteString(data)
	}
	return "Type validation failed: Value: " + compact.String() + ".\nError message: " + strings.TrimSpace(issues.String())
}

// IssueText returns the issue list that follows "Error message: " in a
// validation message, or the whole message when the marker is absent.
func IssueText(message string) string {
	if _, after, ok := strings.Cut(message, "Error message: "); ok {
		return after
	}
	return message
}

// CompactJSON compacts data, or returns it unchanged when it is not JSON.
func CompactJSON(data string) string {
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(data)); err != nil {
		return data
	}
	return b.String()
}
