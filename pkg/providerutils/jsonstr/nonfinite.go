package jsonstr

import (
	"encoding/json"
	"math"
	"reflect"
)

// nonFiniteDepth bounds the copy's recursion (a pointer cycle, which
// encoding/json rejects anyway).
const nonFiniteDepth = 512

// marshalNullNonFinite marshals a deep copy of v in which every NaN / +-Inf
// float is replaced by a finite sentinel the output does not otherwise hold,
// then rewrites each sentinel number token as null. The text otherwise is
// encoding/json's (tags, omitempty, embedding, custom marshalers and key
// order stay). ok is false when no sentinel fits, a custom marshaler hides a
// sentinel (or rewrites it) or the copy does not marshal.
func marshalNullNonFinite(v interface{}) ([]byte, bool) {
	for i := 0; i < 16; i++ {
		s := math.Ldexp(1+float64(i)/64, 103)
		replaced := 0
		cp, ok := replaceNonFinite(reflect.ValueOf(v), s, &replaced, 0)
		if !ok || replaced == 0 {
			return nil, false
		}
		b, err := marshal(cp.Interface())
		if err != nil {
			return nil, false
		}
		out, found := nullSentinelTokens(b, sentinelTexts(s))
		if found == replaced {
			return out, true
		}
	}
	return nil, false
}

// sentinelTexts is the text encoding/json writes for s as a float64 and as a
// float32 (s is exact in both).
func sentinelTexts(s float64) map[string]bool {
	texts := map[string]bool{}
	if b, err := json.Marshal(s); err == nil {
		texts[string(b)] = true
	}
	if b, err := json.Marshal(float32(s)); err == nil {
		texts[string(b)] = true
	}
	return texts
}

// nullSentinelTokens rewrites each number token of JSON text b (outside
// strings) whose text is a sentinel text as null, counting them.
func nullSentinelTokens(b []byte, texts map[string]bool) ([]byte, int) {
	return rewriteNumberTokens(b, func(tok []byte) ([]byte, bool) {
		if texts[string(tok)] {
			return []byte("null"), true
		}
		return nil, false
	})
}

// rewriteNumberTokens replaces each number token of JSON text b (outside
// strings) for which repl reports true with its replacement, counting them.
func rewriteNumberTokens(b []byte, repl func(tok []byte) ([]byte, bool)) ([]byte, int) {
	out := make([]byte, 0, len(b))
	found := 0
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(b) && b[j] != '"' {
				if b[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(b))
			out = append(out, b[i:j]...)
			i = j
		case c == '-' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < len(b) && (b[j] == '.' || b[j] == 'e' || b[j] == 'E' || b[j] == '+' || b[j] == '-' || (b[j] >= '0' && b[j] <= '9')) {
				j++
			}
			if r, ok := repl(b[i:j]); ok {
				out = append(out, r...)
				found++
			} else {
				out = append(out, b[i:j]...)
			}
			i = j
		default:
			out = append(out, c)
			i++
		}
	}
	return out, found
}

// replaceNonFinite returns a deep copy of rv with every NaN / +-Inf float
// that encoding/json can reach (exported struct fields, map values, slice
// and array elements, pointer and interface targets) replaced by s, and
// counts the replacements. ok is false past the depth bound.
func replaceNonFinite(rv reflect.Value, s float64, n *int, depth int) (reflect.Value, bool) {
	if depth > nonFiniteDepth {
		return reflect.Value{}, false
	}
	if !rv.IsValid() {
		return rv, true
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		if f := rv.Float(); math.IsNaN(f) || math.IsInf(f, 0) {
			*n++
			out := reflect.New(rv.Type()).Elem()
			out.SetFloat(s)
			return out, true
		}
		return rv, true
	case reflect.Pointer:
		if rv.IsNil() {
			return rv, true
		}
		elem, ok := replaceNonFinite(rv.Elem(), s, n, depth+1)
		if !ok {
			return reflect.Value{}, false
		}
		out := reflect.New(rv.Type().Elem())
		out.Elem().Set(elem)
		return out, true
	case reflect.Interface:
		if rv.IsNil() {
			return rv, true
		}
		elem, ok := replaceNonFinite(rv.Elem(), s, n, depth+1)
		if !ok {
			return reflect.Value{}, false
		}
		out := reflect.New(rv.Type()).Elem()
		out.Set(elem)
		return out, true
	case reflect.Struct:
		out := reflect.New(rv.Type()).Elem()
		out.Set(rv)
		for i := 0; i < rv.NumField(); i++ {
			f := out.Field(i)
			if !f.CanSet() {
				continue
			}
			v, ok := replaceNonFinite(rv.Field(i), s, n, depth+1)
			if !ok {
				return reflect.Value{}, false
			}
			f.Set(v)
		}
		return out, true
	case reflect.Map:
		if rv.IsNil() {
			return rv, true
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			v, ok := replaceNonFinite(iter.Value(), s, n, depth+1)
			if !ok {
				return reflect.Value{}, false
			}
			out.SetMapIndex(iter.Key(), v)
		}
		return out, true
	case reflect.Slice:
		if rv.IsNil() || rv.Type().Elem().Kind() == reflect.Uint8 {
			return rv, true
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			v, ok := replaceNonFinite(rv.Index(i), s, n, depth+1)
			if !ok {
				return reflect.Value{}, false
			}
			out.Index(i).Set(v)
		}
		return out, true
	case reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return rv, true
		}
		out := reflect.New(rv.Type()).Elem()
		for i := 0; i < rv.Len(); i++ {
			v, ok := replaceNonFinite(rv.Index(i), s, n, depth+1)
			if !ok {
				return reflect.Value{}, false
			}
			out.Index(i).Set(v)
		}
		return out, true
	}
	return rv, true
}
