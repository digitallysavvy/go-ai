package jsonorder

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
)

// nonFiniteSentinelDepth bounds the copy's recursion (a pointer cycle, which
// encoding/json rejects anyway).
const nonFiniteSentinelDepth = 512

// nullNonFiniteViaSentinel is JSON.parse(JSON.stringify(value)) for a value
// whose NaN or ±Inf sits in a struct field (at any depth), which
// nullNonFinite cannot rebuild without reimplementing encoding/json's struct
// rules. It marshals a deep copy in which every non-finite float is replaced
// by a finite sentinel the encoded value does not otherwise hold, then
// decodes it and sets each sentinel number to nil, as JSON.stringify writes
// NaN and Infinity as null. Tags, omitempty, embedding and custom marshalers
// stay encoding/json's. ok is false when no sentinel fits or the value does
// not marshal for another reason.
func nullNonFiniteViaSentinel(value interface{}) (interface{}, bool) {
	for i := 0; i < 16; i++ {
		s := math.Ldexp(1+float64(i)/64, 101)
		replaced := 0
		cp, ok := replaceNonFinite(reflect.ValueOf(value), s, &replaced, 0)
		if !ok || replaced == 0 {
			return nil, false
		}
		data, err := json.Marshal(cp.Interface())
		if err != nil {
			return nil, false
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var tree interface{}
		if err := dec.Decode(&tree); err != nil {
			return nil, false
		}
		texts := sentinelTexts(s)
		found := 0
		out := nullSentinels(tree, texts, &found)
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

// nullSentinels sets every number whose text is a sentinel text to nil
// (counting them) and turns the other numbers into float64, as
// json.Unmarshal into interface{} does.
func nullSentinels(v interface{}, texts map[string]bool, found *int) interface{} {
	switch x := v.(type) {
	case json.Number:
		if texts[string(x)] {
			*found++
			return nil
		}
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil {
			return nil
		}
		return f
	case map[string]interface{}:
		for k, e := range x {
			x[k] = nullSentinels(e, texts, found)
		}
		return x
	case []interface{}:
		for i, e := range x {
			x[i] = nullSentinels(e, texts, found)
		}
		return x
	}
	return v
}

// replaceNonFinite returns a deep copy of rv with every NaN / ±Inf float
// that encoding/json can reach (exported struct fields, map values, slice
// and array elements, pointer and interface targets) replaced by s, and
// counts the replacements. ok is false past the depth bound.
func replaceNonFinite(rv reflect.Value, s float64, n *int, depth int) (reflect.Value, bool) {
	if depth > nonFiniteSentinelDepth {
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
