package google

import (
	"encoding/json"
	"reflect"
	"strings"
)

// looseHook lets a type with its own UnmarshalJSON finish a loose decode
// (for example to keep the wire object next to the typed fields).
type looseHook interface {
	afterLooseDecode(data []byte)
}

// looseDecode decodes data into v (a pointer) as far as the wire shape
// allows: a value whose shape does not fit its Go type is left at its zero
// value, while its siblings are still decoded.
//
// TS parses interactions stream events against a union whose last member is
// the loose `unknownEvent` ({event_type: string}). A known event_type whose
// payload fails its own member schema matches that member, keeps the raw
// object, and is still dispatched by event_type. looseDecode is the Go
// counterpart of that raw dispatch.
func looseDecode(data []byte, v interface{}) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return
	}
	looseDecodeValue(data, rv.Elem())
}

func looseDecodeValue(data []byte, rv reflect.Value) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return
	}
	tmp := reflect.New(rv.Type())
	if json.Unmarshal(data, tmp.Interface()) == nil {
		rv.Set(tmp.Elem())
		return
	}
	switch rv.Kind() {
	case reflect.Ptr:
		elem := reflect.New(rv.Type().Elem())
		if elem.Elem().Kind() == reflect.Struct && !strings.HasPrefix(trimmed, "{") {
			return
		}
		looseDecodeValue(data, elem.Elem())
		rv.Set(elem)
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil {
			return
		}
		t := rv.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := f.Name
			if tag := f.Tag.Get("json"); tag != "" {
				if tag == "-" {
					continue
				}
				if n, _, _ := strings.Cut(tag, ","); n != "" {
					name = n
				}
			}
			if raw, ok := fields[name]; ok {
				looseDecodeValue(raw, rv.Field(i))
			}
		}
		if h, ok := rv.Addr().Interface().(looseHook); ok {
			h.afterLooseDecode(data)
		}
	case reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(data, &items) != nil {
			return
		}
		s := reflect.MakeSlice(rv.Type(), len(items), len(items))
		for i, item := range items {
			looseDecodeValue(item, s.Index(i))
		}
		rv.Set(s)
	}
	// Any other mismatched value (a wrong-typed string, number, bool or
	// map) stays at its zero value.
}
