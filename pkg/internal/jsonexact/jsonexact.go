// Package jsonexact makes encoding/json struct decoding match object keys
// exactly, as zod's object schemas do. encoding/json also matches a key to a
// field case-insensitively ("Message" fills `json:"message"`); zod treats
// such a key as unknown and strips it. Filter drops, at any depth, every
// object key whose only match in the target struct is case-insensitive, so
// the decode sees what zod sees.
package jsonexact

import (
	"bytes"
	"cmp"
	"encoding"
	"encoding/json"
	"reflect"
	"slices"
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

// fields lists the JSON names of t's fields as encoding/json sees them (a
// port of its typeFields): json tag names, Go field names when untagged,
// promoted fields of untagged embedded structs with Go's dominance rules (the
// shallowest field wins, then the tagged one; two at the same depth hide the
// name); unexported and `json:"-"` fields are skipped.
func fields(t reflect.Type) *structFields {
	if f, ok := fieldCache.Load(t); ok {
		return f.(*structFields)
	}
	f := &structFields{exact: map[string]reflect.Type{}}
	for _, fl := range typeFields(t) {
		f.exact[fl.name] = fl.typ
		f.names = append(f.names, fl.name)
	}
	fieldCache.Store(t, f)
	return f
}

type field struct {
	name  string
	tag   bool
	index []int
	typ   reflect.Type
}

func typeFields(t reflect.Type) []field {
	var current []field
	next := []field{{typ: t}}
	var count, nextCount map[reflect.Type]int
	visited := map[reflect.Type]bool{}
	var found []field
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
					et := sf.Type
					if et.Kind() == reflect.Pointer {
						et = et.Elem()
					}
					if !sf.IsExported() && et.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, _, _ := strings.Cut(tag, ",")
				if !validTag(name) {
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
					tagged := name != ""
					if name == "" {
						name = sf.Name
					}
					found = append(found, field{name: name, tag: tagged, index: index, typ: sf.Type})
					if count[f.typ] > 1 {
						found = append(found, found[len(found)-1])
					}
					continue
				}
				nextCount[ft]++
				if nextCount[ft] == 1 {
					next = append(next, field{name: ft.Name(), index: index, typ: ft})
				}
			}
		}
	}
	slices.SortFunc(found, func(a, b field) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		if c := cmp.Compare(len(a.index), len(b.index)); c != 0 {
			return c
		}
		if a.tag != b.tag {
			if a.tag {
				return -1
			}
			return 1
		}
		return slices.Compare(a.index, b.index)
	})
	out := found[:0]
	for advance, i := 0, 0; i < len(found); i += advance {
		fi := found[i]
		for advance = 1; i+advance < len(found) && found[i+advance].name == fi.name; advance++ {
		}
		if advance > 1 && len(fi.index) == len(found[i+1].index) && fi.tag == found[i+1].tag {
			continue // hidden: two dominant candidates
		}
		out = append(out, fi)
	}
	slices.SortFunc(out, func(a, b field) int { return slices.Compare(a.index, b.index) })
	return out
}

// validTag is encoding/json's isValidTag.
func validTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
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
// and reports whether any object key in data fold-matches one of them while
// differing from it. A key exact for one struct can still be only a
// case-insensitive match in another, so an exact match elsewhere does not
// clear it. Most bodies have none and skip the rewrite.
func suspicious(data []byte, t reflect.Type) bool {
	folded := map[string]map[string]bool{} // foldKey -> exact names
	collectNames(t, folded, map[reflect.Type]bool{})
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
				if names := folded[foldKey(key)]; len(names) > 1 || (len(names) == 1 && !names[key]) {
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

func collectNames(t reflect.Type, folded map[string]map[string]bool, seen map[reflect.Type]bool) {
	t = deref(t)
	if seen[t] || custom(t) {
		return
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Struct:
		f := fields(t)
		for name, ft := range f.exact {
			k := foldKey(name)
			if folded[k] == nil {
				folded[k] = map[string]bool{}
			}
			folded[k][name] = true
			collectNames(ft, folded, seen)
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		collectNames(t.Elem(), folded, seen)
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
// options) keeps json.Unmarshal's case-insensitive matching (v0.5.2). A
// number beyond the double range reads as +-Inf in an interface position,
// as JSON.parse reads it (Decode), for struct, map and interface{} targets.
// A payload with a forbidden prototype key fails first with the TS
// JSONParseError (ParseError), as TS secureJsonParse rejects it before its
// schema runs.
func Unmarshal(data []byte, v interface{}) error {
	if err := ParseError(data); err != nil {
		return err
	}
	return decodeJS(Filter(data, v), v)
}

// UnmarshalPlain is Unmarshal without the prototype check: the twin of a TS
// `await response.json()` (plain JSON.parse), which keeps a `__proto__` key
// or a `constructor.prototype` as an ordinary own property instead of
// rejecting the payload as secureJsonParse does. Keys still match exactly,
// and a number beyond the double range reads as +-Inf in an interface
// position. Use Unmarshal wherever TS runs secureJsonParse / safeParseJSON.
func UnmarshalPlain(data []byte, v interface{}) error {
	return decodeJS(Filter(data, v), v)
}

// UntaggedFields lists, as "Type.Field", every exported struct field reachable
// from the targets' types (through pointers, slices, arrays, maps and nested
// structs, not into types with their own UnmarshalJSON / UnmarshalText) that
// has no json tag name and is not an untagged embedded struct. Under Filter
// such a field matches only a wire key spelled exactly like the Go field
// name, so a response type that relied on case-insensitive matching loses
// it. Guard tests call this over the response types decoded with Filter.
func UntaggedFields(targets ...interface{}) []string {
	var out []string
	seen := map[reflect.Type]bool{}
	for _, target := range targets {
		if t := reflect.TypeOf(target); t != nil {
			untagged(t, seen, &out)
		}
	}
	return out
}

func untagged(t reflect.Type, seen map[reflect.Type]bool, out *[]string) {
	t = deref(t)
	if seen[t] || custom(t) {
		return
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			tag := sf.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if sf.Anonymous && name == "" && deref(sf.Type).Kind() == reflect.Struct {
				untagged(sf.Type, seen, out)
				continue
			}
			if !sf.IsExported() {
				continue
			}
			if name == "" {
				*out = append(*out, t.String()+"."+sf.Name)
			}
			untagged(sf.Type, seen, out)
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		untagged(t.Elem(), seen, out)
	}
}
