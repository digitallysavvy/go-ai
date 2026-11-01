// Package zodlite is a minimal port of the zod validation semantics that the
// TS providers apply to parsed streaming frames (anthropicChunkSchema,
// google chunkSchema). zod objects are non-strict (unknown keys pass),
// `nullish` means absent or null, `optional` means absent only, and
// `nullable` means the key must be present but may be null. Like zod, objects,
// arrays and records report every failing entry (in schema or sorted-key
// order). A union follows zod v4: when exactly one member failed only on
// checks (a regex, a bound; issues with Continue set), its issues are
// reported directly; otherwise an invalid_union issue lists each member's
// full issue list. Type, value and key issues abort, as in zod.
package zodlite

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
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
	// Options is the zod 4.4 "options" list of a "No matching
	// discriminator" issue (DiscriminatedUnion only).
	Options []string
	// codeFirst renders an invalid_type issue as {code, expected, ...}, the
	// key order zod 4.4 uses for a discriminated union's non-object input.
	codeFirst bool
	// Keys is the unrecognized_keys "keys" list (a strict object).
	Keys []string
	// Origin, Maximum and Minimum shape a too_big / too_small issue
	// ({origin, code, maximum|minimum, inclusive: true}); Note marks zod's
	// safe-integer range issue ({code, maximum|minimum, note, origin,
	// inclusive}).
	Origin  string
	Maximum int64
	Minimum int64
	Note    string
	// Format is the invalid_type "format" (int: "safeint") or the
	// invalid_format "format" ("regex"); Pattern is the regex as zod
	// renders it ("/^\\d+$/").
	Format  string
	Pattern string
	// Continue marks a check issue (zod `continue: true`: a bound, a regex,
	// a refinement) that does not abort the value; a union whose only
	// non-aborted member failed with such issues reports them directly.
	Continue bool
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
	case i.Expected != "" && i.codeFirst:
		parts = append(parts, `"code":`+enc(i.Code), `"expected":`+enc(i.Expected))
	case i.Expected != "" && i.Format != "":
		parts = append(parts, `"expected":`+enc(i.Expected), `"format":`+enc(i.Format), `"code":`+enc(i.Code))
	case i.Expected != "":
		parts = append(parts, `"expected":`+enc(i.Expected), `"code":`+enc(i.Code))
	case i.Code == "invalid_value":
		vals := i.Values
		if vals == nil {
			vals = []string{}
		}
		parts = append(parts, `"code":`+enc(i.Code), `"values":`+enc(vals))
	case i.Code == "unrecognized_keys":
		parts = append(parts, `"code":`+enc(i.Code), `"keys":`+enc(i.Keys))
	case i.Note != "" && i.Code == "too_big":
		parts = append(parts, `"code":`+enc(i.Code), `"maximum":`+strconv.FormatInt(i.Maximum, 10), `"note":`+enc(i.Note), `"origin":`+enc(i.Origin), `"inclusive":true`)
	case i.Note != "" && i.Code == "too_small":
		parts = append(parts, `"code":`+enc(i.Code), `"minimum":`+strconv.FormatInt(i.Minimum, 10), `"note":`+enc(i.Note), `"origin":`+enc(i.Origin), `"inclusive":true`)
	case i.Code == "too_big":
		parts = append(parts, `"origin":`+enc(i.Origin), `"code":`+enc(i.Code), `"maximum":`+strconv.FormatInt(i.Maximum, 10), `"inclusive":true`)
	case i.Code == "too_small":
		parts = append(parts, `"origin":`+enc(i.Origin), `"code":`+enc(i.Code), `"minimum":`+strconv.FormatInt(i.Minimum, 10), `"inclusive":true`)
	case i.Code == "invalid_format" && i.Format == "regex":
		parts = append(parts, `"origin":"string"`, `"code":`+enc(i.Code), `"format":"regex"`, `"pattern":`+enc(i.Pattern))
	case i.Discriminator != "" && i.Options != nil:
		parts = append(parts, `"code":`+enc(i.Code), `"errors":[]`, `"note":"No matching discriminator"`, `"discriminator":`+enc(i.Discriminator), `"options":`+enc(i.Options))
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

// MaxItems is n (an array schema) with .max(max). Like zod v4, the element
// issues come first and a too_big issue follows when the array is longer
// than max, so both are reported.
func MaxItems(n Node, max int) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		bad := n(v, present, path)
		a, ok := v.([]interface{})
		if !ok || len(a) <= max {
			return bad
		}
		tooBig := &Issue{Path: path, Code: "too_big", Origin: "array", Maximum: int64(max), Continue: true, Message: "Too big: expected array to have <=" + strconv.Itoa(max) + " items"}
		return join([]*Issue{bad, tooBig})
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

// StrictEmptyObject is z.object({}).strict(): an object with no keys. Go maps
// lose wire order, so the unrecognized keys are reported sorted.
func StrictEmptyObject(v interface{}, present bool, path []interface{}) *Issue {
	return StrictObject()(v, present, path)
}

// StrictObject is z.object({...}).strict(): the fields' issues first, then
// one unrecognized_keys issue for the keys the schema does not declare
// (sorted: Go maps lose wire order).
func StrictObject(fields ...Field) Node {
	known := make(map[string]bool, len(fields))
	for _, f := range fields {
		known[f.Key] = true
	}
	inner := Object(fields...)
	return func(v interface{}, present bool, path []interface{}) *Issue {
		bad := inner(v, present, path)
		m, ok := v.(map[string]interface{})
		if !ok {
			return bad
		}
		return join([]*Issue{bad, unrecognizedKeys(m, known, path)})
	}
}

func unrecognizedKeys(m map[string]interface{}, known map[string]bool, path []interface{}) *Issue {
	keys := make([]string, 0, len(m))
	for k := range m {
		if !known[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = `"` + k + `"`
	}
	message := "Unrecognized key: "
	if len(keys) > 1 {
		message = "Unrecognized keys: "
	}
	return &Issue{Path: path, Code: "unrecognized_keys", Keys: keys, Message: message + strings.Join(quoted, ", ")}
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

// Union is z.union: the first matching node wins. As zod v4
// (handleUnionResults), when exactly one member failed without aborting
// (only Continue issues, e.g. a regex or a bound after the type matched),
// that member's issues are the union's issues; otherwise one invalid_union
// issue carries every member's issue list.
func Union(nodes ...Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		var errs [][]*Issue
		nonAborted := -1
		count := 0
		for i, n := range nodes {
			bad := n(v, present, nil)
			if bad == nil {
				return nil
			}
			if !Aborted(bad) {
				nonAborted = i
				count++
			}
			errs = append(errs, bad.all())
		}
		if count == 1 {
			return nodes[nonAborted](v, present, path)
		}
		return &Issue{Path: path, Code: "invalid_union", Errors: errs, Message: "Invalid input"}
	}
}

// Aborted reports whether any issue in the list aborts the value (zod
// util.aborted: an issue without `continue: true`).
func Aborted(bad *Issue) bool {
	for _, it := range bad.all() {
		if !it.Continue {
			return true
		}
	}
	return false
}

// maxSafeInteger is Number.MAX_SAFE_INTEGER.
const maxSafeInteger int64 = 1<<53 - 1

// Int is z.number().int(): a number (else invalid_type "number"), an
// integer (else the aborting invalid_type {expected: "int", format:
// "safeint"}), within the safe integer range (else a too_big / too_small
// check issue with zod's note).
func Int(v interface{}, present bool, path []interface{}) *Issue {
	f, ok := v.(float64)
	if !ok {
		return Invalid("number", v, present, path)
	}
	if math.IsInf(f, 0) || math.IsNaN(f) || math.Trunc(f) != f {
		return &Issue{Path: path, Code: "invalid_type", Expected: "int", Format: "safeint", Message: "Invalid input: expected int, received number"}
	}
	const note = "Integers must be within the safe integer range."
	switch {
	case f > float64(maxSafeInteger):
		return &Issue{Path: path, Code: "too_big", Maximum: maxSafeInteger, Note: note, Origin: "int", Continue: true, Message: "Too big: expected int to be <=" + strconv.FormatInt(maxSafeInteger, 10)}
	case f < -float64(maxSafeInteger):
		return &Issue{Path: path, Code: "too_small", Minimum: -maxSafeInteger, Note: note, Origin: "int", Continue: true, Message: "Too small: expected int to be >=" + strconv.FormatInt(-maxSafeInteger, 10)}
	}
	return nil
}

// Min is n (a number schema) with .min(min) (zod gte, inclusive): unless n
// aborted, a value below min adds a too_small check issue.
func Min(n Node, min int64) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		bad := n(v, present, path)
		if bad != nil && Aborted(bad) {
			return bad
		}
		if f, ok := v.(float64); ok && f < float64(min) {
			small := &Issue{Path: path, Code: "too_small", Origin: "number", Minimum: min, Continue: true, Message: "Too small: expected number to be >=" + strconv.FormatInt(min, 10)}
			return join([]*Issue{bad, small})
		}
		return bad
	}
}

// Max is n (a number schema) with .max(max) (zod lte, inclusive): unless n
// aborted, a value above max adds a too_big check issue.
func Max(n Node, max int64) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		bad := n(v, present, path)
		if bad != nil && Aborted(bad) {
			return bad
		}
		if f, ok := v.(float64); ok && f > float64(max) {
			big := &Issue{Path: path, Code: "too_big", Origin: "number", Maximum: max, Continue: true, Message: "Too big: expected number to be <=" + strconv.FormatInt(max, 10)}
			return join([]*Issue{bad, big})
		}
		return bad
	}
}

// Regex is z.string().regex(/source/): a string (else invalid_type), that
// matches source (else the invalid_format check issue). source must be a
// pattern Go's regexp accepts with the same meaning as in JS.
func Regex(source string) Node {
	re := regexp.MustCompile(source)
	pattern := "/" + source + "/"
	return func(v interface{}, present bool, path []interface{}) *Issue {
		s, ok := v.(string)
		if !ok {
			return Invalid("string", v, present, path)
		}
		if re.MatchString(s) {
			return nil
		}
		return &Issue{Path: path, Code: "invalid_format", Format: "regex", Pattern: pattern, Continue: true, Message: "Invalid string: must match pattern " + pattern}
	}
}

// Lazy is z.lazy(get): get is called on each validation, so a schema can
// refer to itself (a recursive filter tree).
func Lazy(get func() Node) Node {
	return func(v interface{}, present bool, path []interface{}) *Issue {
		return get()(v, present, path)
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

// DiscriminatedUnion is z.discriminatedUnion('type', ...) as zod 4.4
// reports it: options lists the variants' type values in schema order (each
// must have a node in variants). An unknown type is one invalid_union issue
// at path.type with the options and "Invalid discriminator value. Expected
// 'a' | 'b'"; a non-object is invalid_type {code, expected: "object", ...}.
// Disc keeps the zod 4.1-4.3 text the stream frame schemas render.
func DiscriminatedUnion(options []string, variants map[string]Node) Node {
	quoted := make([]string, len(options))
	for i, o := range options {
		quoted[i] = "'" + o + "'"
	}
	message := "Invalid discriminator value. Expected " + strings.Join(quoted, " | ")
	return func(v interface{}, present bool, path []interface{}) *Issue {
		m, ok := v.(map[string]interface{})
		if !ok {
			bad := Invalid("object", v, present, path)
			bad.codeFirst = true
			return bad
		}
		t, _ := m["type"].(string)
		n, known := variants[t]
		if !known {
			return &Issue{Path: append(append([]interface{}{}, path...), "type"), Code: "invalid_union", Discriminator: "type", Options: options, Message: message}
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
