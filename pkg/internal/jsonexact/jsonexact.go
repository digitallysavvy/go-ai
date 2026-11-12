// Package jsonexact makes encoding/json struct decoding match object keys
// exactly, as zod's object schemas do. encoding/json also matches a key to a
// field case-insensitively ("Message" fills `json:"message"`); zod treats
// such a key as unknown and strips it. Filter drops, at any depth, every
// object key whose only match in the target struct is case-insensitive, so
// the decode sees what zod sees.
package jsonexact

import (
	"bytes"
	"encoding"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

var (
	unmarshalerType     = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// Filter returns data with the keys removed that encoding/json would match
// to a field of target's type only case-insensitively. Keys that match a
// field exactly, and keys that match no field at all, are kept. Values under
// a type with its own UnmarshalJSON / UnmarshalText, a map of interface
// values or an interface are left as they are. data is returned unchanged
// (the same slice) when nothing is dropped or when it is not valid JSON, so
// the caller's decode reports any syntax error with its usual text.
func Filter(data []byte, target interface{}) []byte {
	t := reflect.TypeOf(target)
	if t == nil || !hasStruct(t, map[reflect.Type]bool{}) {
		return data
	}
	if !suspicious(data, t) || !json.Valid(data) {
		return data
	}
	out, changed, ok := filterValue(data, t, 0)
	if !ok || !changed {
		return data
	}
	return out
}

const maxDepth = 10000

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func custom(t reflect.Type) bool {
	return t.Implements(unmarshalerType) || reflect.PointerTo(t).Implements(unmarshalerType) ||
		t.Implements(textUnmarshalerType) || reflect.PointerTo(t).Implements(textUnmarshalerType)
}

// hasStruct reports whether decoding into t reaches a struct whose keys
// encoding/json matches itself.
func hasStruct(t reflect.Type, seen map[reflect.Type]bool) bool {
	t = deref(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	if custom(t) {
		return false
	}
	switch t.Kind() {
	case reflect.Struct:
		return true
	case reflect.Slice, reflect.Array, reflect.Map:
		return hasStruct(t.Elem(), seen)
	}
	return false
}

type structFields struct {
	exact map[string]reflect.Type
	names []string
}

var fieldCache sync.Map // reflect.Type -> *structFields

// fields lists the JSON names of t's fields as encoding/json sees them:
// json tag names, Go field names when untagged, promoted fields of untagged
// embedded structs; unexported and `json:"-"` fields are skipped.
func fields(t reflect.Type) *structFields {
	if f, ok := fieldCache.Load(t); ok {
		return f.(*structFields)
	}
	f := &structFields{exact: map[string]reflect.Type{}}
	collectFields(t, f, map[reflect.Type]bool{})
	fieldCache.Store(t, f)
	return f
}

func collectFields(t reflect.Type, f *structFields, visiting map[reflect.Type]bool) {
	if visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if sf.Anonymous && name == "" {
			ft := deref(sf.Type)
			if ft.Kind() == reflect.Struct {
				collectFields(ft, f, visiting)
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		if _, dup := f.exact[name]; !dup {
			f.exact[name] = sf.Type
			f.names = append(f.names, name)
		}
	}
}

// dropped reports whether key would fill a field only case-insensitively.
func (f *structFields) dropped(key string) bool {
	if _, ok := f.exact[key]; ok {
		return false
	}
	for _, name := range f.names {
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

// suspicious is a cheap pass: it collects every field name reachable from t
// and reports whether any object key in data fold-matches one of them
// without matching any exactly. Most bodies have none and skip the rewrite.
func suspicious(data []byte, t reflect.Type) bool {
	exact := map[string]bool{}
	folded := map[string]bool{}
	collectNames(t, exact, folded, map[reflect.Type]bool{})
	if len(folded) == 0 {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	type frame struct{ object, expectKey bool }
	var stack []frame
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		top := len(stack) - 1
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			stack = stack[:top]
			if len(stack) == 0 {
				return false
			}
			continue
		}
		if top >= 0 && stack[top].object {
			if stack[top].expectKey {
				key, _ := tok.(string)
				stack[top].expectKey = false
				if !exact[key] && folded[foldKey(key)] {
					return true
				}
				continue
			}
			// A value: the next token in this object is a key.
			stack[top].expectKey = true
		}
		if d, ok := tok.(json.Delim); ok {
			stack = append(stack, frame{object: d == '{', expectKey: d == '{'})
			continue
		}
		if len(stack) == 0 {
			return false
		}
	}
}

// foldKey maps s to a canonical form shared by exactly the strings that
// strings.EqualFold matches (each rune is replaced by the smallest rune of
// its simple case-folding orbit).
func foldKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		min := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < min {
				min = f
			}
		}
		b.WriteRune(min)
	}
	return b.String()
}

func collectNames(t reflect.Type, exact, folded map[string]bool, seen map[reflect.Type]bool) {
	t = deref(t)
	if seen[t] || custom(t) {
		return
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Struct:
		f := fields(t)
		for name, ft := range f.exact {
			exact[name] = true
			folded[foldKey(name)] = true
			collectNames(ft, exact, folded, seen)
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		collectNames(t.Elem(), exact, folded, seen)
	}
}

// filterValue rewrites one JSON value for decoding into t. ok is false when
// data is not valid JSON.
func filterValue(data []byte, t reflect.Type, depth int) (out []byte, changed, ok bool) {
	if depth > maxDepth {
		return data, false, true
	}
	t = deref(t)
	if custom(t) {
		return data, false, true
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return data, false, false
	}
	switch {
	case t.Kind() == reflect.Struct && trimmed[0] == '{':
		return filterObject(data, depth, func(key string) (reflect.Type, bool) {
			f := fields(t)
			if f.dropped(key) {
				return nil, false
			}
			return f.exact[key], true
		})
	case t.Kind() == reflect.Map && trimmed[0] == '{':
		elem := t.Elem()
		return filterObject(data, depth, func(string) (reflect.Type, bool) { return elem, true })
	case (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) && trimmed[0] == '[':
		var items []json.RawMessage
		if json.Unmarshal(data, &items) != nil {
			return data, false, false
		}
		var buf bytes.Buffer
		buf.WriteByte('[')
		anyChanged := false
		for i, item := range items {
			if i > 0 {
				buf.WriteByte(',')
			}
			v, c, good := filterValue(item, t.Elem(), depth+1)
			if !good {
				return data, false, false
			}
			anyChanged = anyChanged || c
			buf.Write(v)
		}
		buf.WriteByte(']')
		if !anyChanged {
			return data, false, true
		}
		return buf.Bytes(), true, true
	}
	return data, false, true
}

// filterObject rewrites a JSON object, keeping duplicate keys in order (the
// decoder lets the last one win, as JSON.parse does). field returns the type
// a key's value decodes into (nil: not a field), or false to drop the key.
func filterObject(data []byte, depth int, field func(string) (reflect.Type, bool)) ([]byte, bool, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return data, false, false
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	changed, first := false, true
	for dec.More() {
		tok, err := dec.Token()
		key, isKey := tok.(string)
		if err != nil || !isKey {
			return data, false, false
		}
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return data, false, false
		}
		ft, keep := field(key)
		if !keep {
			changed = true
			continue
		}
		value := []byte(raw)
		if ft != nil {
			v, c, good := filterValue(raw, ft, depth+1)
			if !good {
				return data, false, false
			}
			changed = changed || c
			value = v
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(key)
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(value)
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return data, false, false
	}
	buf.WriteByte('}')
	if !changed {
		return data, false, true
	}
	return buf.Bytes(), true, true
}

// Unmarshal is json.Unmarshal with case-exact keys (Filter first). Use it for
// provider response bodies and frames; user-supplied input (provider
// options) keeps json.Unmarshal's case-insensitive matching (v0.5.2).
func Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(Filter(data, v), v)
}
