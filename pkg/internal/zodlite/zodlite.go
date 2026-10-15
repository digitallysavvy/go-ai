// Package zodlite is a minimal port of the zod validation semantics that the
// TS providers apply to parsed streaming frames (anthropicChunkSchema,
// google chunkSchema). zod objects are non-strict (unknown keys pass),
// `nullish` means absent or null, `optional` means absent only, and
// `nullable` means the key must be present but may be null. Like zod, objects,
// arrays and records report every failing entry (in schema or sorted-key
// order); a union reports each member's full issue list.
package zodlite

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// Issue is one validation failure.
type Issue struct {
	Path    []interface{}
	Code    string
	Message string
	// Expected is the invalid_type "expected" field; Values the
	// invalid_value "values" list; Discriminator marks the zod v4
	// "No matching discriminator" union issue. They only shape the rendered
	// issue JSON.
	Expected      string
	Values        []string
	Discriminator string
	// Errors holds, for a plain invalid_union issue, each member's first
	// issue with paths relative to the union value (zod v4 nested `errors`).
	Errors [][]*Issue
	// rest are further issues found in the same value (zod reports all of an
	// object's failing fields, not just the first).
	rest []*Issue
}

// all returns the issue followed by its siblings, each without siblings.
func (i *Issue) all() []*Issue {
	if i == nil {
		return nil
	}
	head := *i
	head.rest = nil
	return append([]*Issue{&head}, i.rest...)
}

// join merges issues (nil entries skipped) into one head issue that carries
// the others as siblings; nil when there are none.
func join(list []*Issue) *Issue {
	var flat []*Issue
	for _, it := range list {
		flat = append(flat, it.all()...)
	}
	if len(flat) == 0 {
		return nil
	}
	head := *flat[0]
	head.rest = flat[1:]
	return &head
}

// json renders the issue like zod v4's ZodIssue object (same key order as
// the object zod builds, so JSON.stringify output matches).
func (i *Issue) json() []byte {
	enc := func(v interface{}) string {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		_ = e.Encode(v)
		return strings.TrimSpace(b.String())
	}
	path := i.Path
	if path == nil {
		path = []interface{}{}
	}
	var parts []string
	switch {
	case i.Expected != "":
		parts = append(parts, `"expected":`+enc(i.Expected), `"code":`+enc(i.Code))
	case i.Code == "invalid_value":
		vals := i.Values
		if vals == nil {
			vals = []string{}
		}
		parts = append(parts, `"code":`+enc(i.Code), `"values":`+enc(vals))
	case i.Discriminator != "":
		parts = append(parts, `"code":`+enc(i.Code), `"errors":[]`, `"note":"No matching discriminator"`, `"discriminator":`+enc(i.Discriminator))
	case i.Code == "invalid_union":
		nested := make([]string, 0, len(i.Errors))
		for _, member := range i.Errors {
			items := make([]string, 0, len(member))
			for _, e := range member {
				items = append(items, string(e.json()))
			}
			nested = append(nested, "["+strings.Join(items, ",")+"]")
		}
		parts = append(parts, `"code":`+enc(i.Code), `"errors":[`+strings.Join(nested, ",")+`]`)
	default:
		parts = append(parts, `"code":`+enc(i.Code))
	}
	parts = append(parts, `"path":`+enc(path), `"message":`+enc(i.Message))
	return []byte("{" + strings.Join(parts, ",") + "}")
}

// Error renders the issue as "path.to[0].key: message" (just the message when
// the path is empty), so an Issue can be used as an error value.
func (i *Issue) Error() string {
	var b strings.Builder
	for n, p := range i.Path {
		switch x := p.(type) {
		case string:
			if n > 0 {
				b.WriteByte('.')
			}
			b.WriteString(x)
		case int:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(x))
			b.WriteByte(']')
		}
	}
	if b.Len() == 0 {
		return i.Message
	}
	return b.String() + ": " + i.Message
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
	return &Issue{Path: path, Code: "invalid_type", Expected: expected, Message: "Invalid input: expected " + expected + ", received " + JSType(v, present)}
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
		// zod's stringifyPrimitive wraps the raw string in quotes (no escaping).
		return &Issue{Path: path, Code: "invalid_value", Values: []string{want}, Message: `Invalid input: expected "` + want + `"`}
	}
}

// Enum is z.enum(values...): one of the listed strings.
func Enum(values ...string) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		if s, ok := v.(string); ok {
			for _, w := range values {
				if s == w {
					return nil
				}
			}
		}
		quoted := make([]string, len(values))
		for i, w := range values {
			quoted[i] = `"` + w + `"`
		}
		if len(values) == 1 { // zod v4 en locale: a single value reads like a literal
			return &Issue{Path: path, Code: "invalid_value", Values: values, Message: "Invalid input: expected " + quoted[0]}
		}
		return &Issue{Path: path, Code: "invalid_value", Values: values, Message: "Invalid option: expected one of " + strings.Join(quoted, "|")}
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
		var bad []*Issue
		for i, e := range a {
			bad = append(bad, n(e, true, append(append([]interface{}{}, path...), i)))
		}
		return join(bad)
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
		var bad []*Issue
		for _, f := range fields {
			fv, fp := m[f.Key]
			bad = append(bad, f.Node(fv, fp, append(append([]interface{}{}, path...), f.Key)))
		}
		return join(bad)
	}
}

// Record is z.record(z.string(), z.unknown()): an object with any values.
func Record(v interface{}, present bool, path []interface{}) *Issue {
	if _, ok := v.(map[string]interface{}); !ok {
		return Invalid("record", v, present, path)
	}
	return nil
}

// RecordOf is z.record(z.string(), n).
func RecordOf(n Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return Invalid("record", v, present, path)
		}
		// Go maps lose wire order; sorted keys keep the report deterministic.
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var bad []*Issue
		for _, k := range keys {
			bad = append(bad, n(m[k], true, append(append([]interface{}{}, path...), k)))
		}
		return join(bad)
	}
}

// Union is z.union: the first matching node wins.
func Union(nodes ...Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		var errs [][]*Issue
		for _, n := range nodes {
			bad := n(v, present, nil)
			if bad == nil {
				return nil
			}
			errs = append(errs, bad.all())
		}
		return &Issue{Path: path, Code: "invalid_union", Errors: errs, Message: "Invalid input"}
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
			return &Issue{Path: append(append([]interface{}{}, path...), "type"), Code: "invalid_union", Discriminator: "type", Message: "Invalid input"}
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
	var issues bytes.Buffer
	// zod v4's ZodError.message is JSON.stringify(issues, null, 2).
	items := make([]string, 0, 1+len(bad.rest))
	for _, it := range bad.all() {
		items = append(items, string(it.json()))
	}
	arr := []byte("[" + strings.Join(items, ",") + "]")
	if err := json.Indent(&issues, arr, "", "  "); err != nil {
		issues.Reset()
		issues.Write(arr)
	}
	return "Type validation failed: Value: " + stringify(data) + ".\nError message: " + issues.String()
}

// stringify is JSON.stringify of the parsed frame: wire key order kept,
// \u escapes decoded, numbers re-rendered the JS way (1.0 -> 1).
func stringify(data string) string {
	tree, ok := jsonorder.Parse([]byte(data))
	if !ok {
		return CompactJSON(data)
	}
	return jsonorder.Stringify(jsNumbers(tree))
}

func jsNumbers(v interface{}) interface{} {
	switch x := v.(type) {
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
	case []interface{}:
		for i := range x {
			x[i] = jsNumbers(x[i])
		}
	case *jsonorder.Object:
		for _, k := range x.Keys() {
			e, _ := x.Get(k)
			x.Set(k, jsNumbers(e))
		}
	}
	return v
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
