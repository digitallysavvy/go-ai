// Package securejson is the Go twin of @ai-sdk/provider-utils safeParseJSON /
// secureJsonParse: JSON text parses only when no object (at any depth) has an
// own `__proto__` key or a `constructor` object with an own `prototype` key.
package securejson

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// ErrForbiddenPrototype is secure-json-parse's SyntaxError text.
var ErrForbiddenPrototype = errors.New("Object contains forbidden prototype property") //nolint:staticcheck // matches the TS error text

// Check returns ErrForbiddenPrototype when data (valid JSON) holds a forbidden
// key. Invalid JSON is not an error here: the caller's own decode reports it.
func Check(data []byte) error {
	// Fast path: the keys can only appear literally or through \u escapes.
	if !bytes.Contains(data, []byte("__proto__")) && !bytes.Contains(data, []byte("constructor")) &&
		!bytes.Contains(data, []byte(`\u`)) {
		return nil
	}
	// UseNumber: a number out of the double range (JS Infinity) must not
	// skip the check, which would let such a body carry a forbidden key.
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return nil
	}
	if forbidden(v) {
		return ErrForbiddenPrototype
	}
	return nil
}

func forbidden(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["__proto__"]; ok {
			return true
		}
		if c, ok := x["constructor"].(map[string]any); ok {
			if _, has := c["prototype"]; has {
				return true
			}
		}
		for _, e := range x {
			if forbidden(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if forbidden(e) {
				return true
			}
		}
	}
	return false
}

// Unmarshal is json.Unmarshal that first rejects forbidden prototype keys,
// with object keys matched to struct fields exactly (jsonexact), as the zod
// schema that follows every TS safeParseJSON / secureJsonParse does: a
// differently cased key fills no field.
func Unmarshal(data []byte, v any) error {
	if err := Check(data); err != nil {
		return err
	}
	return jsonexact.Unmarshal(data, v)
}
