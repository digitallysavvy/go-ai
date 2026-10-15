package providerutils

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// Key-order helpers. A JavaScript object keeps its insertion order, so TS
// sends JSON-schema `properties` (which structured-output models follow when
// generating), injected schema text and JSONL lines in author order. Go maps
// sort their keys. When the caller hands a schema as raw JSON
// (json.RawMessage) the order is known; these helpers carry it to the wire.
// Maps stay sorted (deterministic) because they carry no order.

// RawJSONObject returns v as raw JSON when it is a json.RawMessage (or
// []byte) holding a JSON object, boolean or other valid JSON value.
func RawJSONObject(v interface{}) (json.RawMessage, bool) {
	var raw []byte
	switch x := v.(type) {
	case json.RawMessage:
		raw = x
	case []byte:
		raw = x
	default:
		return nil, false
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, false
	}
	return json.RawMessage(raw), true
}

// DecodeRawJSONObject decodes raw JSON into a generic map (nil when raw is
// not an object).
func DecodeRawJSONObject(raw json.RawMessage) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// CompactJSON is JSON.stringify of an already-parsed raw value: whitespace
// removed, key order kept.
func CompactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// OrderedMap marshals its keys in the given order (then any keys missing from
// Keys, sorted).
type OrderedMap struct {
	Keys []string
	Vals map[string]interface{}
}

// MarshalJSON implements json.Marshaler.
func (o OrderedMap) MarshalJSON() ([]byte, error) {
	seen := make(map[string]bool, len(o.Keys))
	keys := make([]string, 0, len(o.Vals))
	for _, k := range o.Keys {
		if _, ok := o.Vals[k]; ok && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range o.Vals {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(o.Vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// orderNode is the key-order skeleton of a raw JSON value.
type orderNode struct {
	keys     []string
	children map[string]*orderNode
	elems    []*orderNode
}

func readOrderNode(dec *json.Decoder) *orderNode {
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return &orderNode{}
	}
	n := &orderNode{}
	switch d {
	case '{':
		n.children = map[string]*orderNode{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return n
			}
			k, _ := kt.(string)
			child := readOrderNode(dec)
			if _, dup := n.children[k]; !dup {
				n.keys = append(n.keys, k)
			}
			n.children[k] = child
		}
		_, _ = dec.Token()
	case '[':
		for dec.More() {
			n.elems = append(n.elems, readOrderNode(dec))
		}
		_, _ = dec.Token()
	}
	return n
}

func applyOrder(v interface{}, n *orderNode) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		var keys []string
		var children map[string]*orderNode
		if n != nil {
			keys, children = n.keys, n.children
		}
		for k, val := range x {
			out[k] = applyOrder(val, children[k])
		}
		return OrderedMap{Keys: keys, Vals: out}
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			var child *orderNode
			if n != nil && i < len(n.elems) {
				child = n.elems[i]
			}
			out[i] = applyOrder(val, child)
		}
		return out
	}
	return v
}

// OrderLike returns v (a decoded or transformed JSON tree) with every object
// marshaling its keys in the order they have in ref; keys that ref does not
// have follow, sorted. Arrays are paired with ref by index.
func OrderLike(v interface{}, ref json.RawMessage) interface{} {
	n := readOrderNode(json.NewDecoder(bytes.NewReader(ref)))
	return applyOrder(v, n)
}

// BatchJSONLLine is one line of a batch input file with the key order TS
// writes ({ custom_id, method, url, body }).
func BatchJSONLLine(customID, method, url string, body interface{}) ([]byte, error) {
	return OrderedMap{
		Keys: []string{"custom_id", "method", "url", "body"},
		Vals: map[string]interface{}{"custom_id": customID, "method": method, "url": url, "body": body},
	}.MarshalJSON()
}

// schemaInstructionText serializes a schema for an injected instruction:
// raw JSON keeps its order, anything else is JSON.stringify of the value
// (maps sorted, no HTML escaping).
func schemaInstructionText(schema interface{}) (string, bool) {
	if raw, ok := RawJSONObject(schema); ok {
		return CompactJSON(raw), true
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(schema); err != nil {
		return "", false
	}
	return strings.TrimSuffix(buf.String(), "\n"), true
}

// ResponseFormatSchemaOrRaw resolves a ResponseFormat.Schema to the value a
// provider can serialize in author order: a non-nil map or raw JSON. Anything
// else (nil, nil map, unknown type) gives nil.
func ResponseFormatSchemaOrRaw(value interface{}) interface{} {
	switch s := ResponseFormatJSONSchema(value).(type) {
	case map[string]interface{}:
		if s == nil {
			return nil
		}
		return s
	case json.RawMessage, []byte:
		if raw, ok := RawJSONObject(s); ok {
			return raw
		}
	}
	return nil
}

// ArgumentsJSONText is JSON.stringify(input) for tool-call arguments. raw is
// the wire text the arguments were parsed from; when it still decodes to the
// same value as args, the text keeps the wire key order (TS stringifies the
// parsed object, which keeps insertion order). Otherwise args is serialized
// with sorted keys.
func ArgumentsJSONText(args map[string]interface{}, raw string) string {
	if raw != "" && json.Valid([]byte(raw)) {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil && parsed != nil {
			if args == nil || reflect.DeepEqual(parsed, args) {
				return CompactJSON(json.RawMessage(raw))
			}
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(args); err != nil {
		return "{}"
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
