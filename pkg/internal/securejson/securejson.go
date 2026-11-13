// Package securejson is the Go twin of @ai-sdk/provider-utils safeParseJSON /
// secureJsonParse: JSON text parses only when no object (at any depth) has an
// own `__proto__` key or a `constructor` object with an own `prototype` key.
package securejson

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// ErrForbiddenPrototype is secure-json-parse's SyntaxError text.
var ErrForbiddenPrototype = jsonexact.ErrForbiddenPrototype

// Check returns ErrForbiddenPrototype when data (valid JSON) holds a forbidden
// key. Invalid JSON is not an error here: the caller's own decode reports it.
func Check(data []byte) error { return jsonexact.CheckPrototype(data) }

// Unmarshal is json.Unmarshal that first rejects forbidden prototype keys,
// with object keys matched to struct fields exactly (jsonexact), as the zod
// schema that follows every TS safeParseJSON / secureJsonParse does: a
// differently cased key fills no field.
func Unmarshal(data []byte, v any) error {
	return jsonexact.Unmarshal(data, v)
}
