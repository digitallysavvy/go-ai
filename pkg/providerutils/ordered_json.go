package providerutils

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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
// Keys, sorted), without HTML escaping, like JSON.stringify of a JS object.
// It is a thin builder over jsonorder.Object, the SDK's one ordered-JSON
// implementation.
type OrderedMap struct {
	Keys []string
	Vals map[string]interface{}
}

// MarshalJSON implements json.Marshaler.
func (o OrderedMap) MarshalJSON() ([]byte, error) {
	return o.object().MarshalJSON()
}

func (o OrderedMap) object() *jsonorder.Object {
	obj := jsonorder.New()
	for _, k := range o.Keys {
		if v, ok := o.Vals[k]; ok {
			if _, dup := obj.Get(k); !dup {
				obj.Set(k, v)
			}
		}
	}
	var rest []string
	for k := range o.Vals {
		if _, seen := obj.Get(k); !seen {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		obj.Set(k, o.Vals[k])
	}
	return obj
}

// OrderLike returns v (a decoded or transformed JSON tree) so that it
// marshals every object's keys in the order they have in ref; keys that ref
// does not have follow, sorted. Arrays are paired with ref by index. It wraps
// jsonorder.Hinted, the SDK's one implementation of this.
func OrderLike(v interface{}, ref json.RawMessage) interface{} {
	hint, ok := jsonorder.Parse(ref)
	if !ok {
		return v
	}
	return jsonorder.Hinted{V: v, Hint: hint}
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
	b, err := jsonstr.Marshal(schema)
	if err != nil {
		return "", false
	}
	return string(b), true
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
	b, err := jsonstr.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}
