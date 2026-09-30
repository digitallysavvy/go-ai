package codemode

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// toJSONPayload JSON-encodes a Go value produced outside the sandbox (a
// host tool's output) for delivery into JavaScript, enforcing a byte-size
// limit. It mirrors TypeScript's toJsonPayload/toStrictJsonPayload
// (code-mode/src/utils/serialization.ts), including JSON.stringify's
// undefined-omission and Date-to-ISO-string semantics, approximated in Go
// as described on compactUndefined and encodeJSONValue.
//
// A nil value returns "" (mirroring TS's `value === undefined` shortcut);
// fromJSONPayload treats "" as the reverse.
func toJSONPayload(value interface{}, maxBytes int, label string) (string, error) {
	if value == nil {
		return "", nil
	}

	encoded, err := marshalJSCompatible(value)
	if err != nil {
		return "", NewError(
			fmt.Sprintf("%s is not JSON-serializable: %s", label, err.Error()),
			"CODE_MODE_SERIALIZATION_ERROR",
			nil,
		)
	}

	if err := assertJSONPayloadSize(encoded, maxBytes, label); err != nil {
		return "", err
	}
	return encoded, nil
}

// fromJSONPayload decodes a JSON payload produced by toJSONPayload (or read
// from the sandbox) back into a Go value. "" decodes to nil, mirroring
// TypeScript's fromJsonPayload.
func fromJSONPayload(valueJSON string) (interface{}, error) {
	if valueJSON == "" {
		return nil, nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(valueJSON), &v); err != nil {
		return nil, NewError(fmt.Sprintf("invalid JSON payload: %s", err.Error()), "CODE_MODE_PROTOCOL_ERROR", nil)
	}
	return v, nil
}

// assertJSONPayloadSize mirrors TypeScript's assertJsonPayloadSize.
func assertJSONPayloadSize(valueJSON string, maxBytes int, label string) error {
	bytes := len([]byte(valueJSON))
	if maxBytes > 0 && bytes > maxBytes {
		return NewError(
			fmt.Sprintf("%s exceeds the %d byte size limit.", label, maxBytes),
			"CODE_MODE_SERIALIZATION_ERROR",
			map[string]interface{}{"bytes": bytes, "maxBytes": maxBytes},
		)
	}
	return nil
}

// errCircularReference is returned by jsCompatibleValue when a
// map[string]interface{} or []interface{} contains itself, directly or
// transitively. Go, unlike encoding/json on plain interface{} graphs, does
// not detect this on its own and would otherwise recurse until the stack
// overflows (a fatal, unrecoverable Go runtime error -- not a panic
// classifySandboxFailure/recover could catch), so jsCompatibleValue tracks
// the current ancestor chain itself.
var errCircularReference = errors.New("circular reference")

// marshalJSCompatible encodes a Go value the way JavaScript's
// JSON.stringify would encode the equivalent value for the cases the
// ported TypeScript test suite exercises for host tool outputs:
//
//   - time.Time (and *time.Time) values encode as their ECMA-262
//     Date#toISOString() form ("1970-01-01T00:00:00.000Z"), not Go's
//     RFC3339Nano.
//   - map[string]interface{} entries whose value is nil are omitted,
//     approximating JSON.stringify's `undefined`-valued-property omission
//     (Go has no distinct null/undefined: a Go nil is treated as
//     "undefined" and dropped; an explicit JSON `null` decoded from a
//     provider response survives as-is since it round-trips through Go as
//     a non-nil json.RawMessage-free `interface{}` wrapping nil only when
//     it, too, was nil -- this is a known, documented limitation).
//   - a map or slice that contains itself (directly or transitively)
//     fails with errCircularReference, mirroring JSON.stringify's
//     "Converting circular structure to JSON" TypeError.
//
// Every other Go value encodes via encoding/json, which already matches
// JSON.stringify for plain JSON-shaped data (objects, arrays, strings,
// finite numbers, bools, null).
func marshalJSCompatible(value interface{}) (string, error) {
	converted, err := jsCompatibleValue(value, map[uintptr]bool{})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(converted)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func jsCompatibleValue(value interface{}, ancestors map[uintptr]bool) (interface{}, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return jsISOString(v), nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return jsISOString(*v), nil
	case map[string]interface{}:
		if len(v) == 0 {
			return map[string]interface{}{}, nil
		}
		ptr := reflect.ValueOf(v).Pointer()
		if ancestors[ptr] {
			return nil, errCircularReference
		}
		ancestors[ptr] = true
		defer delete(ancestors, ptr)

		out := make(map[string]interface{}, len(v))
		for k, val := range v {
			if val == nil {
				continue
			}
			converted, err := jsCompatibleValue(val, ancestors)
			if err != nil {
				return nil, err
			}
			out[k] = converted
		}
		return out, nil
	case []interface{}:
		if len(v) == 0 {
			return []interface{}{}, nil
		}
		ptr := reflect.ValueOf(v).Pointer()
		if ancestors[ptr] {
			return nil, errCircularReference
		}
		ancestors[ptr] = true
		defer delete(ancestors, ptr)

		out := make([]interface{}, len(v))
		for i, val := range v {
			converted, err := jsCompatibleValue(val, ancestors)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil
	default:
		return value, nil
	}
}

// jsISOString formats t the way JavaScript's Date#toISOString() does:
// always UTC, always exactly millisecond precision.
func jsISOString(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
