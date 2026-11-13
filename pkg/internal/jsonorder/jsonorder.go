// Package jsonorder keeps JSON object key order, the way a JavaScript object
// and JSON.stringify do. Go maps sort their keys, which changes the text of
// tool inputs and JSON answers that TS passes through in wire order.
package jsonorder

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// scalar is JS JSON.stringify for a string, bool or null (compact, no HTML
// escaping of <, > and &).
func scalar(v interface{}) string {
	if b, err := jsonstr.Marshal(v); err == nil {
		return string(b)
	}
	b, _ := json.Marshal(v) //nolint:errcheck // fallback for unencodable values
	return string(b)
}

// Object is a JSON object that keeps the key order of the wire, like a
// JavaScript object does. TS builds tool inputs with JSON.stringify over the
// parsed response, so the text keeps the wire order; a Go map would sort the
// keys and change the text the model (and callers) see.
type Object struct {
	keys []string
	vals map[string]interface{}
}

// isArrayIndex reports whether k is a canonical JS array index ("0" or digits
// without a leading zero, below 2^32-1), the keys a JS object enumerates first.
func isArrayIndex(k string) (uint64, bool) {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return 0, false
		}
		n = n*10 + uint64(k[i]-'0')
	}
	return n, n < 1<<32-1
}

// JSKeyOrder returns keys in the order a JavaScript object enumerates them
// (Object.keys, JSON.stringify): array-index keys first in ascending numeric
// order, then the other keys in the given (insertion) order. The input slice
// is returned unchanged when it has no array-index key.
func JSKeyOrder(keys []string) []string {
	idx := 0
	for _, k := range keys {
		if _, ok := isArrayIndex(k); ok {
			idx++
		}
	}
	if idx == 0 {
		return keys
	}
	nums := make([]string, 0, idx)
	rest := make([]string, 0, len(keys)-idx)
	for _, k := range keys {
		if _, ok := isArrayIndex(k); ok {
			nums = append(nums, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.SliceStable(nums, func(i, j int) bool {
		a, _ := isArrayIndex(nums[i])
		b, _ := isArrayIndex(nums[j])
		return a < b
	})
	return append(nums, rest...)
}

// Parse parses raw JSON into an ordered tree: *Object for
// objects, []interface{} for arrays and json.Number, string, bool or nil for
// scalars.
func Parse(raw []byte) (interface{}, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, ok := decode(dec, 0)
	if !ok {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		// Trailing data, a token or a syntax error: JSON.parse throws.
		return nil, false
	}
	return v, true
}

// maxParseDepth is encoding/json's maxNestingDepth: deeper input fails to
// parse instead of overflowing the goroutine stack (a fatal, unrecoverable
// error; V8 throws a catchable RangeError for the same input).
const maxParseDepth = 10000

func decode(dec *json.Decoder, depth int) (interface{}, bool) {
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	d, isDelim := tok.(json.Delim)
	if !isDelim {
		return tok, true
	}
	if depth >= maxParseDepth {
		return nil, false
	}
	switch d {
	case '{':
		o := &Object{vals: map[string]interface{}{}}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, false
			}
			key, _ := kt.(string)
			v, ok := decode(dec, depth+1)
			if !ok {
				return nil, false
			}
			if _, dup := o.vals[key]; !dup {
				o.keys = append(o.keys, key)
			}
			o.vals[key] = v
		}
		if _, err := dec.Token(); err != nil {
			return nil, false
		}
		o.keys = JSKeyOrder(o.keys)
		return o, true
	case '[':
		arr := []interface{}{}
		for dec.More() {
			v, ok := decode(dec, depth+1)
			if !ok {
				return nil, false
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, false
		}
		return arr, true
	}
	return nil, false
}

// Stringify is JSON.stringify for an ordered tree.
func Stringify(v interface{}) string {
	switch x := v.(type) {
	case *Object:
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(scalar(k))
			b.WriteByte(':')
			b.WriteString(Stringify(x.vals[k]))
		}
		b.WriteByte('}')
		return b.String()
	case []interface{}:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(Stringify(e))
		}
		b.WriteByte(']')
		return b.String()
	case json.Number:
		return jsNumberText(x)
	}
	return scalar(v)
}

// jsNumberText is JSON.stringify(JSON.parse(text)) for a JSON number: the
// value goes through a double, so 1.0 and 1e2 read 1 and 100, -0 reads 0,
// digits beyond double precision are rounded, and a number outside the double
// range (Infinity) reads null.
func jsNumberText(n json.Number) string {
	f, err := n.Float64()
	if err != nil && (math.IsInf(f, 0) || math.IsNaN(f)) {
		return "null"
	}
	if err != nil {
		return n.String()
	}
	if f == 0 {
		return "0"
	}
	b, err := json.Marshal(f)
	if err != nil {
		return n.String()
	}
	return string(b)
}

// FromMap builds an ordered object from a map with sorted keys, for
// inputs that have no raw wire form.
func FromMap(m map[string]interface{}) *Object {
	o := &Object{vals: map[string]interface{}{}}
	for k := range m {
		o.keys = append(o.keys, k)
	}
	sort.Strings(o.keys)
	o.keys = JSKeyOrder(o.keys)
	for k, v := range m {
		o.vals[k] = v
	}
	return o
}

// Input returns the tool input object in wire order. fallback is used
// when the raw form is absent or not an object.
func Input(raw json.RawMessage, fallback map[string]interface{}) *Object {
	if len(raw) > 0 {
		if v, ok := Parse(raw); ok {
			if o, ok := v.(*Object); ok {
				return o
			}
		}
	}
	if fallback == nil {
		fallback = map[string]interface{}{}
	}
	return FromMap(fallback)
}

// WithLead is JS `{ [key]: val, ...o }`: key comes first; a value for the
// same key in o wins.
func (o *Object) WithLead(key string, val interface{}) *Object {
	out := &Object{keys: []string{key}, vals: map[string]interface{}{key: val}}
	for _, k := range o.keys {
		if k == key {
			out.vals[key] = o.vals[k]
			continue
		}
		out.keys = append(out.keys, k)
		out.vals[k] = o.vals[k]
	}
	out.keys = JSKeyOrder(out.keys)
	return out
}

func (o *Object) String() string { return Stringify(o) }

func (o *Object) Len() int { return len(o.keys) }

// Text is JSON.stringify(part.input) for a response block input.
func Text(raw json.RawMessage, fallback map[string]interface{}) string {
	return Input(raw, fallback).String()
}

// New returns an empty object.
func New() *Object { return &Object{vals: map[string]interface{}{}} }

// Keys returns the keys in insertion (wire) order.
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Get returns the value for key.
func (o *Object) Get(key string) (interface{}, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Set sets key; a new key goes last, an existing key keeps its position (JS
// property assignment).
func (o *Object) Set(key string, val interface{}) {
	if o.vals == nil {
		o.vals = map[string]interface{}{}
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = JSKeyOrder(append(o.keys, key))
	}
	o.vals[key] = val
}

// Delete removes key.
func (o *Object) Delete(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// MarshalJSON writes the object in key order, without HTML escaping, so
// json.Marshal and the provider request encoders keep the order.
func (o *Object) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	return []byte(Stringify(o)), nil
}

// UnmarshalJSON parses an object keeping its key order.
func (o *Object) UnmarshalJSON(data []byte) error {
	v, ok := Parse(data)
	parsed, isObj := v.(*Object)
	if !ok || !isObj {
		return errors.New("jsonorder: not a JSON object")
	}
	*o = *parsed
	return nil
}

// Plain converts an ordered tree to plain Go values: map[string]interface{}
// for objects, float64 for numbers (as encoding/json decodes them). The order
// is dropped; use it for validation and for code that needs maps.
func Plain(v interface{}) interface{} {
	switch x := v.(type) {
	case *Object:
		m := make(map[string]interface{}, len(x.keys))
		for _, k := range x.keys {
			m[k] = Plain(x.vals[k])
		}
		return m
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = Plain(e)
		}
		return out
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return x.String()
		}
		return f
	}
	return v
}

// Map returns the object as a plain map (order dropped).
func (o *Object) Map() map[string]interface{} {
	m, _ := Plain(o).(map[string]interface{})
	return m
}

// ToJSONValue round-trips value through JSON, as TS toJSONValue
// (JSON.parse(JSON.stringify(value))): nil and a marshal failure give nil,
// and a json.RawMessage or ordered object keeps its key order (Normalize).
// The tool model output of GenerateText, the legacy agent loop and the
// response messages share it.
func ToJSONValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	if ordered, ok := Normalize(value); ok {
		return ordered
	}
	data, err := json.Marshal(value)
	if err != nil {
		// JSON.stringify writes NaN and Infinity as null; encoding/json
		// rejects the whole value. Retry with them replaced in plain trees.
		if isCyclic(value) {
			// JSON.stringify throws on a circular value; the result is the
			// marshal-failure nil, without the NaN retries (which would
			// recurse through the cycle).
			return nil
		}
		data, err = json.Marshal(nullNonFinite(value))
		if err != nil {
			// A NaN in a struct field: marshal a copy holding a sentinel and
			// null it after decoding (cr-r87e-gen-to-carry 3).
			if out, ok := nullNonFiniteViaSentinel(value); ok {
				return out
			}
			return nil
		}
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// Normalize is the order-preserving case of a tool-result JSON round trip:
// a json.RawMessage (or an *Object) that holds a JSON object or array comes
// back as an ordered tree whose marshaled text keeps the source key order, as
// JSON.parse(JSON.stringify(value)) does in TS. ok is false for any other
// value, which the caller round-trips through plain Go values.
func Normalize(v interface{}) (interface{}, bool) {
	switch x := v.(type) {
	case *Object:
		return x, x != nil
	case json.RawMessage:
		tree, ok := Parse(x)
		if !ok {
			return nil, false
		}
		switch tree.(type) {
		case *Object, []interface{}:
			return tree, true
		}
		return Plain(tree), true
	}
	return nil, false
}

// nullNonFinite returns a copy of a plain JSON tree (maps, slices, scalars)
// with NaN and ±Inf numbers replaced by nil, as JSON.stringify writes them.
// Other values are returned unchanged.
func nullNonFinite(v interface{}) interface{} {
	return nullNonFiniteDepth(v, 0)
}

// unencodable fails json.Marshal; nullNonFiniteDepth returns it past the
// depth bound so the retry fails like the first marshal did.
type unencodable struct{}

func (unencodable) MarshalJSON() ([]byte, error) {
	return nil, errors.New("jsonorder: value nested too deeply")
}

func nullNonFiniteDepth(v interface{}, depth int) interface{} {
	if depth > nonFiniteSentinelDepth {
		return unencodable{}
	}
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
	case float32:
		if f := float64(x); math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, e := range x {
			out[k] = nullNonFiniteDepth(e, depth+1)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = nullNonFiniteDepth(e, depth+1)
		}
		return out
	case []float64:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = nullNonFiniteDepth(e, depth+1)
		}
		return out
	}
	return nullNonFiniteReflect(reflect.ValueOf(v), v, depth)
}

// nullNonFiniteReflect covers the typed containers the switch above does not
// name (map[string]float64, []float32, [][]float64, pointers to them, named
// float types): string-keyed maps, slices and arrays become plain maps and
// slices with their non-finite numbers set to nil. Byte slices, structs and
// every other value are returned as orig (a struct holding NaN is handled by
// nullNonFiniteViaSentinel).
func nullNonFiniteReflect(rv reflect.Value, orig interface{}, depth int) interface{} {
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		if f := rv.Float(); math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return orig
		}
		return nullNonFiniteDepth(rv.Elem().Interface(), depth+1)
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String || rv.IsNil() {
			return orig
		}
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = nullNonFiniteDepth(iter.Value().Interface(), depth+1)
		}
		return out
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 || (rv.Kind() == reflect.Slice && rv.IsNil()) {
			return orig
		}
		out := make([]interface{}, rv.Len())
		for i := range out {
			out[i] = nullNonFiniteDepth(rv.Index(i).Interface(), depth+1)
		}
		return out
	}
	return orig
}

// refKey identifies a map, slice or pointer target for cycle detection.
type refKey struct {
	ptr uintptr
	typ reflect.Type
}

// isCyclic reports whether v reaches itself through maps, slices, pointers
// or interfaces (a JS circular structure), or nests deeper than
// maxParseDepth. Fully explored references are memoized, so shared
// (acyclic) references are visited once.
func isCyclic(v interface{}) bool {
	onPath := map[refKey]bool{}
	done := map[refKey]bool{}
	var walk func(rv reflect.Value, depth int) bool
	walk = func(rv reflect.Value, depth int) bool {
		if depth > maxParseDepth {
			return true
		}
		if !rv.IsValid() {
			return false
		}
		var key refKey
		switch rv.Kind() {
		case reflect.Map, reflect.Pointer:
			if rv.IsNil() {
				return false
			}
			key = refKey{rv.Pointer(), rv.Type()}
		case reflect.Slice:
			if rv.IsNil() || rv.Len() == 0 {
				return false
			}
			key = refKey{rv.Pointer(), rv.Type()}
		case reflect.Interface:
			if rv.IsNil() {
				return false
			}
			return walk(rv.Elem(), depth+1)
		case reflect.Struct:
			for i := 0; i < rv.NumField(); i++ {
				// encoding/json reaches exported fields only.
				if !rv.Type().Field(i).IsExported() {
					continue
				}
				if walk(rv.Field(i), depth+1) {
					return true
				}
			}
			return false
		case reflect.Array:
			for i := 0; i < rv.Len(); i++ {
				if walk(rv.Index(i), depth+1) {
					return true
				}
			}
			return false
		default:
			return false
		}
		if onPath[key] {
			return true
		}
		if done[key] {
			return false
		}
		onPath[key] = true
		defer func() { delete(onPath, key); done[key] = true }()
		switch rv.Kind() {
		case reflect.Map:
			iter := rv.MapRange()
			for iter.Next() {
				if walk(iter.Value(), depth+1) {
					return true
				}
			}
		case reflect.Pointer:
			return walk(rv.Elem(), depth+1)
		case reflect.Slice:
			if rv.Type().Elem().Kind() == reflect.Uint8 {
				return false
			}
			for i := 0; i < rv.Len(); i++ {
				if walk(rv.Index(i), depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(reflect.ValueOf(v), 0)
}
