package schema

import (
	"encoding"
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// jsonSchemaDraft07 is the $schema TS zodSchema emits (zod v4 toJSONSchema
// with target 'draft-7').
const jsonSchemaDraft07 = "http://json-schema.org/draft-07/schema#"

// structSchemaCache holds the JSON Schema of a StructValidator's type,
// generated once: wire is the schema sent to the model (ordered JSON text),
// check is the variant Validate applies to a decoded value.
type structSchemaCache struct {
	once  sync.Once
	wire  []byte
	wireM map[string]interface{}
	check map[string]interface{}
}

func (v *StructValidator) schemas() *structSchemaCache {
	c := v.cache
	if c == nil {
		c = &structSchemaCache{}
	}
	c.once.Do(func() {
		wire := newStructSchemaBuilder(v.targetType, true).build()
		c.wire, _ = wire.MarshalJSON()
		c.wireM = wire.Map()
		c.check = newStructSchemaBuilder(v.targetType, false).build().Map()
	})
	return c
}

// structSchemaBuilder generates the JSON Schema of a Go type the way TS
// zodSchema(z.object(...)) does for the matching zod schema (zod v4
// toJSONSchema, target draft-7, io input, inline reuse, then
// addAdditionalPropertiesToJsonSchema): "$schema" on the root; an object is
// type, properties (in encoding/json field order), required, then
// additionalProperties false; a recursive type is a $ref, "#" for the root
// and "#/definitions/__schemaN" for any other type.
//
// wire selects the schema sent to the model. The other variant, used by
// Validate, leaves out additionalProperties false: zod object parsing strips
// unknown keys rather than rejecting them, as encoding/json ignores them.
type structSchemaBuilder struct {
	root      reflect.Type
	wire      bool
	recursive map[reflect.Type]bool
	defNames  map[reflect.Type]string
	defs      *jsonorder.Object
}

func newStructSchemaBuilder(root reflect.Type, wire bool) *structSchemaBuilder {
	return &structSchemaBuilder{root: derefType(root), wire: wire, defNames: map[reflect.Type]string{}}
}

func (b *structSchemaBuilder) build() *jsonorder.Object {
	if b.root == nil {
		// No type to reflect: v0.5 sent a bare object schema.
		o := jsonorder.New()
		o.Set("type", "object")
		return o
	}
	b.recursive = findRecursiveTypes(b.root)
	out := jsonorder.New()
	out.Set("$schema", jsonSchemaDraft07)
	var body *jsonorder.Object
	if b.root.Kind() == reflect.Struct && !isSpecialType(b.root) {
		body = b.objectSchema(b.root)
	} else {
		body = b.typeSchema(b.root, fieldTags{})
	}
	for _, k := range body.Keys() {
		val, _ := body.Get(k)
		out.Set(k, val)
	}
	if b.defs != nil {
		// zod emit(): the definitions come after the root schema, and
		// addAdditionalPropertiesToJsonSchema appends additionalProperties
		// afterwards when the root object has none yet.
		if ap, ok := out.Get("additionalProperties"); ok {
			out.Delete("additionalProperties")
			out.Set("definitions", b.defs)
			out.Set("additionalProperties", ap)
		} else {
			out.Set("definitions", b.defs)
		}
	}
	return out
}

var (
	timeType            = reflect.TypeOf(time.Time{})
	rawMessageType      = reflect.TypeOf(json.RawMessage(nil))
	jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// isSpecialType reports a type encoding/json reads with its own decoder, so
// its fields are not the JSON shape.
func isSpecialType(t reflect.Type) bool {
	if t == timeType || t == rawMessageType {
		return true
	}
	pt := reflect.PointerTo(t)
	return pt.Implements(jsonUnmarshalerType) || pt.Implements(textUnmarshalerType)
}

// findRecursiveTypes returns the struct types that occur inside themselves.
func findRecursiveTypes(root reflect.Type) map[reflect.Type]bool {
	rec := map[reflect.Type]bool{}
	onStack := map[reflect.Type]bool{}
	done := map[reflect.Type]bool{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		t = derefType(t)
		switch t.Kind() {
		case reflect.Slice, reflect.Array:
			walk(t.Elem())
			return
		case reflect.Map:
			walk(t.Elem())
			return
		case reflect.Struct:
		default:
			return
		}
		if isSpecialType(t) {
			return
		}
		if onStack[t] {
			rec[t] = true
			return
		}
		if done[t] {
			return
		}
		onStack[t] = true
		for _, f := range jsonFields(t) {
			walk(f.typ)
		}
		onStack[t] = false
		done[t] = true
	}
	walk(root)
	return rec
}

// objectSchema is a struct's object schema (zod z.object).
func (b *structSchemaBuilder) objectSchema(t reflect.Type) *jsonorder.Object {
	o := jsonorder.New()
	o.Set("type", "object")
	props := jsonorder.New()
	var required []interface{}
	for _, f := range jsonFields(t) {
		ps := b.fieldSchema(f)
		if ps == nil {
			continue
		}
		props.Set(f.name, ps)
		if f.required() {
			required = append(required, f.name)
		}
	}
	o.Set("properties", props)
	if len(required) > 0 {
		o.Set("required", required)
	}
	if b.wire {
		o.Set("additionalProperties", false)
	}
	return o
}

// fieldSchema is a property's schema, or nil for a field JSON cannot carry
// (chan, func, complex, a non-empty interface).
func (b *structSchemaBuilder) fieldSchema(f structField) *jsonorder.Object {
	tags := f.tags
	if f.quoted {
		switch derefType(f.typ).Kind() {
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
			reflect.Float32, reflect.Float64:
			// json ",string": the value is a JSON string.
			s := jsonorder.New()
			s.Set("type", "string")
			b.setDescription(s, f.description)
			return s
		}
	}
	s := b.typeSchema(f.typ, tags)
	if s == nil {
		return nil
	}
	if f.nullable() && s.Len() > 0 {
		// zod .nullish(): anyOf the value and null.
		n := jsonorder.New()
		n.Set("type", "null")
		wrapped := jsonorder.New()
		wrapped.Set("anyOf", []interface{}{s, n})
		s = wrapped
	}
	b.setDescription(s, f.description)
	return s
}

func (b *structSchemaBuilder) setDescription(s *jsonorder.Object, d string) {
	if d != "" {
		s.Set("description", d)
	}
}

// typeSchema is the schema of a Go type (pointers dereferenced), with the
// field's validate constraints applied.
func (b *structSchemaBuilder) typeSchema(t reflect.Type, tags fieldTags) *jsonorder.Object {
	t = derefType(t)
	s := jsonorder.New()
	switch {
	case t == timeType:
		s.Set("type", "string")
		s.Set("format", "date-time")
		return s
	case t == rawMessageType:
		return s // any JSON value (zod z.any())
	case reflect.PointerTo(t).Implements(jsonUnmarshalerType):
		return s // its own JSON decoding: any value
	case reflect.PointerTo(t).Implements(textUnmarshalerType):
		s.Set("type", "string")
		return s
	}
	switch t.Kind() {
	case reflect.Bool:
		s.Set("type", "boolean")
	case reflect.String:
		s.Set("type", "string")
		tags.applyString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		s.Set("type", "integer")
		tags.applyNumber(s, t)
	case reflect.Float32, reflect.Float64:
		s.Set("type", "number")
		tags.applyNumber(s, t)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && !reflect.PointerTo(t.Elem()).Implements(jsonUnmarshalerType) && !reflect.PointerTo(t.Elem()).Implements(textUnmarshalerType) {
			// encoding/json: a []byte is a base64 string.
			s.Set("type", "string")
			return s
		}
		fallthrough
	case reflect.Array:
		items := b.typeSchema(t.Elem(), fieldTags{})
		if items == nil {
			return nil
		}
		s.Set("type", "array")
		s.Set("items", items)
		tags.applyCount(s, "minItems", "maxItems")
	case reflect.Map:
		keySchema := mapKeySchema(t.Key())
		if keySchema == nil {
			return nil
		}
		values := b.typeSchema(t.Elem(), fieldTags{})
		if values == nil {
			return nil
		}
		// zod z.record(z.string(), value).
		s.Set("type", "object")
		s.Set("propertyNames", keySchema)
		s.Set("additionalProperties", values)
		tags.applyCount(s, "minProperties", "maxProperties")
	case reflect.Struct:
		if t != b.root && !b.recursive[t] {
			return b.objectSchema(t)
		}
		if t == b.root {
			s.Set("$ref", "#")
			return s
		}
		s.Set("$ref", "#/definitions/"+b.defName(t))
		return s
	case reflect.Interface:
		if t.NumMethod() == 0 {
			return s // any JSON value
		}
		return nil
	default:
		return nil
	}
	return s
}

// defName returns the definitions name of a recursive type, generating its
// definition on first use (zod names them __schema0, __schema1, ...).
func (b *structSchemaBuilder) defName(t reflect.Type) string {
	if name, ok := b.defNames[t]; ok {
		return name
	}
	name := "__schema" + strconv.Itoa(len(b.defNames))
	b.defNames[t] = name
	if b.defs == nil {
		b.defs = jsonorder.New()
	}
	b.defs.Set(name, jsonorder.New()) // reserve the position
	b.defs.Set(name, b.objectSchema(t))
	return name
}

func mapKeySchema(k reflect.Type) *jsonorder.Object {
	s := jsonorder.New()
	s.Set("type", "string")
	if k.Kind() == reflect.String {
		return s
	}
	if reflect.PointerTo(k).Implements(textUnmarshalerType) {
		return s
	}
	switch k.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s.Set("pattern", "^-?[0-9]+$")
		return s
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		s.Set("pattern", "^[0-9]+$")
		return s
	}
	return nil
}

// structField is one JSON field of a struct, as encoding/json sees it.
type structField struct {
	name        string
	tagged      bool
	index       []int
	typ         reflect.Type
	quoted      bool
	description string
	tags        fieldTags
	// schemaRequired is jsonschema:"required".
	schemaRequired bool
}

// required decides the field's place in "required" the way Validate treats
// a missing key: encoding/json leaves a missing field at its zero value and
// only validate:"required" rejects it, so a field is required when it has
// validate:"required" (or jsonschema:"required", which Validate then
// enforces through the schema). json omitempty/omitzero and pointers are
// optional unless so marked.
func (f structField) required() bool {
	return f.tags.required || f.schemaRequired
}

// nullable reports a pointer field that may be null (encoding/json decodes
// null into nil): zod .nullish(). validate:"required" rejects a nil pointer,
// so such a field is not nullable.
func (f structField) nullable() bool {
	return f.typ.Kind() == reflect.Pointer && !f.tags.required
}

// jsonFields is encoding/json's typeFields: the exported fields of t,
// embedded structs promoted by the Go visibility rules with json tag names,
// in encoding/json's field order.
func jsonFields(t reflect.Type) []structField {
	type item struct {
		typ   reflect.Type
		index []int
	}
	var current []item
	next := []item{{typ: t}}
	var count, nextCount map[reflect.Type]int
	visited := map[reflect.Type]bool{}
	var fields []structField
	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[reflect.Type]int{}
		for _, f := range current {
			if visited[f.typ] {
				continue
			}
			visited[f.typ] = true
			for i := 0; i < f.typ.NumField(); i++ {
				sf := f.typ.Field(i)
				if sf.Anonymous {
					ft := sf.Type
					if ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					if !sf.IsExported() && ft.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, opts, _ := strings.Cut(tag, ",")
				if !validJSONTagName(name) {
					name = ""
				}
				index := make([]int, len(f.index)+1)
				copy(index, f.index)
				index[len(f.index)] = i
				ft := sf.Type
				if ft.Name() == "" && ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					field := structField{
						name:        name,
						tagged:      name != "",
						index:       index,
						typ:         sf.Type,
						quoted:      hasTagOption(opts, "string"),
						description: fieldDescription(sf.Tag),
						tags:        parseValidateTag(sf.Tag.Get("validate")),
					}
					field.schemaRequired = jsonschemaTagHas(sf.Tag.Get("jsonschema"), "required")
					if field.name == "" {
						field.name = sf.Name
					}
					fields = append(fields, field)
					if count[f.typ] > 1 {
						// Two embeddings of the same type at one depth
						// annihilate each other (encoding/json).
						fields = append(fields, fields[len(fields)-1])
					}
					continue
				}
				nextCount[ft]++
				if nextCount[ft] == 1 {
					next = append(next, item{typ: ft, index: index})
				}
			}
		}
	}
	sort.SliceStable(fields, func(i, j int) bool {
		x := fields
		if x[i].name != x[j].name {
			return x[i].name < x[j].name
		}
		if len(x[i].index) != len(x[j].index) {
			return len(x[i].index) < len(x[j].index)
		}
		if x[i].tagged != x[j].tagged {
			return x[i].tagged
		}
		return indexLess(x[i].index, x[j].index)
	})
	out := fields[:0]
	for advance, i := 0, 0; i < len(fields); i += advance {
		fi := fields[i]
		advance = 1
		for advance < len(fields)-i && fields[i+advance].name == fi.name {
			advance++
		}
		if advance == 1 {
			out = append(out, fi)
			continue
		}
		if dominant, ok := dominantField(fields[i : i+advance]); ok {
			out = append(out, dominant)
		}
	}
	fields = out
	sort.Slice(fields, func(i, j int) bool { return indexLess(fields[i].index, fields[j].index) })
	return fields
}

func indexLess(a, b []int) bool {
	for k, x := range a {
		if k >= len(b) {
			return false
		}
		if x != b[k] {
			return x < b[k]
		}
	}
	return len(a) < len(b)
}

func dominantField(fields []structField) (structField, bool) {
	if len(fields) > 1 && len(fields[0].index) == len(fields[1].index) && fields[0].tagged == fields[1].tagged {
		return structField{}, false
	}
	return fields[0], true
}

func validJSONTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !isLetterOrDigit(c):
			return false
		}
	}
	return true
}

func isLetterOrDigit(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c > 127
}

func hasTagOption(opts, want string) bool {
	for opts != "" {
		var o string
		o, opts, _ = strings.Cut(opts, ",")
		if o == want {
			return true
		}
	}
	return false
}

// fieldDescription reads jsonschema:"description=...", the
// jsonschema_description tag or a description tag (zod .describe()).
func fieldDescription(tag reflect.StructTag) string {
	for _, part := range splitUnescapedCommas(tag.Get("jsonschema")) {
		if v, ok := strings.CutPrefix(part, "description="); ok {
			return v
		}
	}
	if d := tag.Get("jsonschema_description"); d != "" {
		return d
	}
	return tag.Get("description")
}

func jsonschemaTagHas(tag, key string) bool {
	for _, part := range splitUnescapedCommas(tag) {
		if part == key {
			return true
		}
	}
	return false
}

// splitUnescapedCommas splits a jsonschema tag on commas not escaped as \,.
func splitUnescapedCommas(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == ',':
			cur.WriteByte(',')
			i++
		case s[i] == ',':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(parts, cur.String())
}

// fieldTags are the go-playground validate rules of a field that JSON
// Schema can state without ambiguity: required, min/max/len/gt/gte/lt/lte
// and oneof. Rules after "dive" (they apply to the elements), alternatives
// ("a|b") and every other rule are left out of the schema; Validate still
// enforces them.
type fieldTags struct {
	required bool
	min, max *float64
	exMin    *float64
	exMax    *float64
	length   *float64
	oneof    []string
}

func parseValidateTag(tag string) fieldTags {
	var t fieldTags
	for _, rule := range strings.Split(tag, ",") {
		if rule == "dive" || strings.HasPrefix(rule, "keys") {
			break
		}
		if strings.Contains(rule, "|") {
			continue
		}
		name, param, _ := strings.Cut(rule, "=")
		if name == "required" {
			t.required = true
			continue
		}
		if name == "oneof" {
			t.oneof = parseOneOf(param)
			continue
		}
		n, err := strconv.ParseFloat(param, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			continue
		}
		switch name {
		case "min", "gte":
			t.min = tighterMin(t.min, n)
		case "max", "lte":
			t.max = tighterMax(t.max, n)
		case "gt":
			t.exMin = &n
		case "lt":
			t.exMax = &n
		case "len":
			t.length = &n
		}
	}
	return t
}

var oneofValueRE = regexp.MustCompile(`'[^']*'|\S+`)

// parseOneOf splits a go-playground oneof parameter: space separated,
// single quotes around a value with spaces.
func parseOneOf(param string) []string {
	var out []string
	for _, v := range oneofValueRE.FindAllString(param, -1) {
		out = append(out, strings.Trim(v, "'"))
	}
	return out
}

func tighterMin(cur *float64, n float64) *float64 {
	if cur == nil || n > *cur {
		return &n
	}
	return cur
}

func tighterMax(cur *float64, n float64) *float64 {
	if cur == nil || n < *cur {
		return &n
	}
	return cur
}

// applyString maps the rules to string keywords (go-playground counts
// runes, as minLength/maxLength count code points).
func (t fieldTags) applyString(s *jsonorder.Object) {
	lo, hi := t.min, t.max
	if t.length != nil {
		lo, hi = tighterMin(lo, *t.length), tighterMax(hi, *t.length)
	}
	if t.exMin != nil {
		lo = tighterMin(lo, math.Floor(*t.exMin)+1)
	}
	if t.exMax != nil {
		hi = tighterMax(hi, math.Ceil(*t.exMax)-1)
	}
	if lo != nil && *lo > 0 {
		s.Set("minLength", int64(math.Ceil(*lo)))
	}
	if hi != nil && *hi >= 0 {
		s.Set("maxLength", int64(math.Floor(*hi)))
	}
	if len(t.oneof) > 0 {
		enum := make([]interface{}, len(t.oneof))
		for i, v := range t.oneof {
			enum[i] = v
		}
		s.Set("enum", enum)
	}
}

// applyNumber maps the rules to number keywords, with the value range of a
// sized integer kind (encoding/json rejects a value outside it).
func (t fieldTags) applyNumber(s *jsonorder.Object, typ reflect.Type) {
	lo, hi := t.min, t.max
	if kLo, kHi, ok := integerKindRange(typ.Kind()); ok {
		lo, hi = tighterMin(lo, kLo), tighterMax(hi, kHi)
	} else if isUnsignedKind(typ.Kind()) {
		lo = tighterMin(lo, 0)
	}
	if lo != nil {
		s.Set("minimum", jsonNumber(*lo))
	}
	if hi != nil {
		s.Set("maximum", jsonNumber(*hi))
	}
	if t.exMin != nil {
		s.Set("exclusiveMinimum", jsonNumber(*t.exMin))
	}
	if t.exMax != nil {
		s.Set("exclusiveMaximum", jsonNumber(*t.exMax))
	}
	if len(t.oneof) > 0 {
		var enum []interface{}
		for _, v := range t.oneof {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return // not a number list: leave it to Validate
			}
			enum = append(enum, jsonNumber(n))
		}
		s.Set("enum", enum)
	}
}

// applyCount maps the rules to an array or object size.
func (t fieldTags) applyCount(s *jsonorder.Object, minKey, maxKey string) {
	lo, hi := t.min, t.max
	if t.length != nil {
		lo, hi = tighterMin(lo, *t.length), tighterMax(hi, *t.length)
	}
	if t.exMin != nil {
		lo = tighterMin(lo, math.Floor(*t.exMin)+1)
	}
	if t.exMax != nil {
		hi = tighterMax(hi, math.Ceil(*t.exMax)-1)
	}
	if lo != nil && *lo > 0 {
		s.Set(minKey, int64(math.Ceil(*lo)))
	}
	if hi != nil && *hi >= 0 {
		s.Set(maxKey, int64(math.Floor(*hi)))
	}
}

func jsonNumber(f float64) interface{} {
	if f == math.Trunc(f) && math.Abs(f) < 1<<53 {
		return int64(f)
	}
	return f
}

func isUnsignedKind(k reflect.Kind) bool {
	switch k {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}

// integerKindRange is the range of an 8, 16 or 32 bit integer kind.
func integerKindRange(k reflect.Kind) (float64, float64, bool) {
	switch k {
	case reflect.Int8:
		return math.MinInt8, math.MaxInt8, true
	case reflect.Int16:
		return math.MinInt16, math.MaxInt16, true
	case reflect.Int32:
		return math.MinInt32, math.MaxInt32, true
	case reflect.Uint8:
		return 0, math.MaxUint8, true
	case reflect.Uint16:
		return 0, math.MaxUint16, true
	case reflect.Uint32:
		return 0, math.MaxUint32, true
	}
	return 0, 0, false
}
