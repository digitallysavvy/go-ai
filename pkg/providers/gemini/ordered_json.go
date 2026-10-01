package gemini

import (
	"encoding/json"
	"strings"
)

// orderedMap is an insertion-order-preserving JSON object, mirroring the key
// ordering guarantee of a plain JavaScript object. TS's GoogleJSONAccumulator
// (google-json-accumulator.ts) builds `accumulatedArgs` as a plain JS object,
// whose keys iterate/serialize in insertion order; JS's own `JSON.parse` also
// preserves the wire order of object keys. A Go `map[string]interface{}`
// marshaled with encoding/json sorts keys alphabetically, which would break
// the invariant that concatenated tool-input-delta text chunks equal the
// final JSON string. orderedMap (plus []interface{} for arrays) is used
// everywhere a Gemini-streamed JSON value needs to round-trip with its
// original key order intact.
type orderedMap struct {
	keys   []string
	values map[string]interface{}
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: map[string]interface{}{}}
}

func (m *orderedMap) get(key string) (interface{}, bool) {
	v, ok := m.values[key]
	return v, ok
}

func (m *orderedMap) set(key string, value interface{}) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// marshalJSONCompact serializes a scalar Go value (string/float64/bool/nil/
// json.Number/etc.) the way JS `JSON.stringify` does for a single value: no
// indentation and no HTML-escaping of `<`, `>`, `&`.
func marshalJSONCompact(v interface{}) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// marshalOrdered compact-serializes an ordered JSON tree (*orderedMap /
// []interface{} / scalars) with object keys in insertion order, matching
// JS `JSON.stringify`.
func marshalOrdered(v interface{}) string {
	switch val := v.(type) {
	case *orderedMap:
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range val.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(marshalJSONCompact(k))
			b.WriteByte(':')
			b.WriteString(marshalOrdered(val.values[k]))
		}
		b.WriteByte('}')
		return b.String()
	case []interface{}:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range val {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(marshalOrdered(e))
		}
		b.WriteByte(']')
		return b.String()
	case nil:
		return "null"
	default:
		return marshalJSONCompact(val)
	}
}

// toPlainJSON converts an ordered JSON tree into plain Go values
// (map[string]interface{} / []interface{} / scalars) for callers that only
// need structural value equality, not key order (e.g. GoogleJSONAccumulator's
// CurrentJSON result, used by tool-call Arguments and by tests).
func toPlainJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case *orderedMap:
		out := make(map[string]interface{}, len(val.keys))
		for _, k := range val.keys {
			out[k] = toPlainJSON(val.values[k])
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, e := range val {
			out[i] = toPlainJSON(e)
		}
		return out
	default:
		return val
	}
}

// decodeOrderedJSON parses JSON text into an order-preserving tree
// (*orderedMap / []interface{} / scalars), mirroring JS `JSON.parse`, which
// preserves object key insertion order from the source text.
func decodeOrderedJSON(data []byte) (interface{}, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	return decodeOrderedValue(dec)
}

func decodeOrderedValue(dec *json.Decoder) (interface{}, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeOrderedToken(dec, tok)
}

func decodeOrderedToken(dec *json.Decoder, tok json.Token) (interface{}, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := newOrderedMap()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyTok.(string)
				val, err := decodeOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				obj.set(key, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []interface{}{}
			for dec.More() {
				val, err := decodeOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, nil
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		// string, bool, or nil (JSON null decodes to a nil json.Token).
		return t, nil
	}
}
