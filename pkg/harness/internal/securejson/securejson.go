// Package securejson keeps the harness import path of the shared secure JSON
// parser (pkg/internal/securejson, the Go twin of @ai-sdk/provider-utils
// safeParseJSON / secureJsonParse).
package securejson

import shared "github.com/digitallysavvy/go-ai/pkg/internal/securejson"

// ErrForbiddenPrototype is secure-json-parse's SyntaxError text.
var ErrForbiddenPrototype = shared.ErrForbiddenPrototype

// Check returns ErrForbiddenPrototype when data (valid JSON) holds a forbidden
// key. Invalid JSON is not an error here: the caller's own decode reports it.
func Check(data []byte) error { return shared.Check(data) }

// Unmarshal is json.Unmarshal that first rejects forbidden prototype keys.
func Unmarshal(data []byte, v any) error { return shared.Unmarshal(data, v) }
