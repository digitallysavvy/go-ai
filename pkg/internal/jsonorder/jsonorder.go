// Package jsonorder keeps JSON object key order, the way a JavaScript object
// and JSON.stringify do. Go maps sort their keys, which changes the text of
// tool inputs and JSON answers that TS passes through in wire order.
package jsonorder

import (
	"bytes"
	"encoding/json"
	"errors"
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

// Parse parses raw JSON into an ordered tree: *Object for
// objects, []interface{} for arrays and json.Number, string, bool or nil for
// scalars.
func Parse(raw []byte) (interface{}, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, ok := decode(dec)
	if !ok {
		return nil, false
	}
	if _, err := dec.Token(); err == nil {
		return nil, false // trailing data
	}
	return v, true
}

func decode(dec *json.Decoder) (interface{}, bool) {
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	d, isDelim := tok.(json.Delim)
	if !isDelim {
		return tok, true
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
			v, ok := decode(dec)
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
		return o, true
	case '[':
		arr := []interface{}{}
		for dec.More() {
			v, ok := decode(dec)
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
		return x.String()
	}
	return scalar(v)
}

// FromMap builds an ordered object from a map with sorted keys, for
// inputs that have no raw wire form.
func FromMap(m map[string]interface{}) *Object {
	o := &Object{vals: map[string]interface{}{}}
	for k := range m {
		o.keys = append(o.keys, k)
	}
	sort.Strings(o.keys)
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
		o.keys = append(o.keys, key)
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
