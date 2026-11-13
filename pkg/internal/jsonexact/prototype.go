package jsonexact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrForbiddenPrototype is secure-json-parse's SyntaxError text, which TS
// secureJsonParse throws for a payload with a forbidden prototype key.
var ErrForbiddenPrototype = errors.New("Object contains forbidden prototype property") //nolint:staticcheck // matches the TS error text

// CheckPrototype is TS secureJsonParse's scan (provider-utils
// secure-json-parse.ts): it returns ErrForbiddenPrototype when valid JSON
// data has, at any depth, an object with an own `__proto__` key or a
// `constructor` object with an own `prototype` key. Invalid JSON is not an
// error here: the caller's own decode reports it. Bodies without
// `__proto__`, `constructor` or a \u escape skip the scan.
func CheckPrototype(data []byte) error {
	if !bytes.Contains(data, []byte("__proto__")) && !bytes.Contains(data, []byte("constructor")) &&
		!bytes.Contains(data, []byte(`\u`)) {
		return nil
	}
	// UseNumber: a number out of the double range must not skip the scan.
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return nil
	}
	if forbiddenPrototype(v, 0) {
		return ErrForbiddenPrototype
	}
	return nil
}

func forbiddenPrototype(v any, depth int) bool {
	if depth > maxDepth {
		return false
	}
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
			if forbiddenPrototype(e, depth+1) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if forbiddenPrototype(e, depth+1) {
				return true
			}
		}
	}
	return false
}

// JSONParseError is TS AI_JSONParseError (providererrors.JSONParseError is
// this type): text that should have been JSON could not be parsed, here
// because secureJsonParse rejected it. Callers match it with errors.As.
type JSONParseError struct {
	// Text is the text that failed to parse.
	Text string

	// Cause is the underlying parser error.
	Cause error
}

// Error renders the TypeScript message: `JSON parsing failed: Text: <text>.`
// followed by `\nError message: <cause>`.
func (e *JSONParseError) Error() string {
	msg := fmt.Sprintf("JSON parsing failed: Text: %s.", e.Text)
	if e.Cause != nil {
		msg += "\nError message: " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the parser error.
func (e *JSONParseError) Unwrap() error { return e.Cause }

// ParseError is TS safeParseJSON's secureJsonParse failure for data: a
// *JSONParseError wrapping ErrForbiddenPrototype when data holds a forbidden
// prototype key, else nil (see CheckPrototype).
func ParseError(data []byte) error {
	if err := CheckPrototype(data); err != nil {
		return &JSONParseError{Text: string(data), Cause: err}
	}
	return nil
}
