package mcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// The TS MCP schemas are z.looseObject / .loose() (passthrough): keys the
// schema does not name survive parsing. Go structs only have the named
// fields, so each loose type carries the rest in an Extra map that the
// decoder fills and the encoder writes back.

// jsonFieldNames returns the wire names of the struct type's exported
// fields (from their json tags).
func jsonFieldNames(t reflect.Type) map[string]bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	names := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			name = f.Name
		}
		names[name] = true
	}
	return names
}

// decodeLooseExtra returns the keys of the JSON object data that the struct
// type of known does not declare.
func decodeLooseExtra(data []byte, known interface{}) map[string]interface{} {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	names := jsonFieldNames(reflect.TypeOf(known))
	var extra map[string]interface{}
	for k, v := range raw {
		if names[k] {
			continue
		}
		var val interface{}
		if err := json.Unmarshal(v, &val); err != nil {
			continue
		}
		if extra == nil {
			extra = map[string]interface{}{}
		}
		extra[k] = val
	}
	return extra
}

// marshalLoose encodes base (a struct value) and appends the extra keys that
// base does not declare, in sorted order.
func marshalLoose(base interface{}, extra map[string]interface{}) ([]byte, error) {
	out, err := json.Marshal(base)
	if err != nil || len(extra) == 0 {
		return out, err
	}
	names := jsonFieldNames(reflect.TypeOf(base))
	keys := make([]string, 0, len(extra))
	for k := range extra {
		if !names[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	body := bytes.TrimSuffix(out, []byte("}"))
	buf.Write(body)
	needComma := len(bytes.TrimSpace(body)) > 1
	for _, k := range keys {
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(extra[k])
		if err != nil {
			return nil, err
		}
		if needComma {
			buf.WriteByte(',')
		}
		needComma = true
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func encodeLooseExtra(extra map[string]interface{}) string {
	if len(extra) == 0 {
		return ""
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return ""
	}
	return string(b)
}

func decodeLooseExtraJSON(s string) map[string]interface{} {
	if s == "" {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
