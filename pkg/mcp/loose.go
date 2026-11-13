package mcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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
		// zod copies a passthrough value as parsed: the entry decoder has
		// applied the prototype check where TS secure-parses, and a
		// response.json() body keeps a nested __proto__ key.
		if err := jsonexact.UnmarshalPlain(v, &val); err != nil {
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
	out, err := jsonstr.Marshal(base)
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
		kb, _ := jsonstr.Marshal(k)
		vb, err := jsonstr.Marshal(extra[k])
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

// looseExtraRaw is the JSON object of the members decodeLooseExtra returns,
// written from the server's own JSON text: the undeclared members are kept
// as sent, so a value that does not survive a Go re-encode (a number beyond
// the double range, which JSON.parse reads as Infinity) is still there for
// Extra().
func looseExtraRaw(data []byte, known interface{}) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	names := jsonFieldNames(reflect.TypeOf(known))
	keys := make([]string, 0, len(raw))
	for k := range raw {
		if !names[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := jsonstr.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		if json.Compact(&buf, raw[k]) != nil {
			return ""
		}
	}
	buf.WriteByte('}')
	return buf.String()
}

func decodeLooseExtraJSON(s string) map[string]interface{} {
	if s == "" {
		return nil
	}
	// Member by member, as decodeLooseExtra: one value that does not decode
	// does not hide the others.
	return decodeLooseExtra([]byte(s), struct{}{})
}
