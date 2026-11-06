// Package jsonstr serializes values like JavaScript's JSON.stringify: HTML
// characters (<, > and &) stay literal instead of being escaped as < etc.
// Use it for JSON text the model sees (tool-call arguments, tool results).
package jsonstr

import (
	"bytes"
	"encoding/json"
)

// Marshal is json.Marshal without HTML escaping and without a trailing
// newline. U+2028 and U+2029 are written as is, like JSON.stringify (Go's
// encoder always escapes them).
func Marshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return unescapeLineSeparators(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

var (
	escapedLineSeparator      = []byte(`\u2028`)
	escapedParagraphSeparator = []byte(`\u2029`)
)

// unescapeLineSeparators replaces the escapes \u2028 and \u2029 the Go
// encoder writes with the characters themselves. An escape is only a real one
// when an even number of backslashes precedes its own backslash; `\\u2028` is
// a backslash followed by the text "u2028".
func unescapeLineSeparators(b []byte) []byte {
	if !bytes.Contains(b, escapedLineSeparator) && !bytes.Contains(b, escapedParagraphSeparator) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		if b[i] != '\\' {
			out = append(out, b[i])
			i++
			continue
		}
		// b[i] starts an escape sequence (the encoder escapes every
		// backslash it writes), so look at the pair.
		if i+6 <= len(b) && b[i+1] == 'u' && b[i+2] == '2' && b[i+3] == '0' && b[i+4] == '2' && (b[i+5] == '8' || b[i+5] == '9') {
			if b[i+5] == '8' {
				out = append(out, "\u2028"...)
			} else {
				out = append(out, "\u2029"...)
			}
			i += 6
			continue
		}
		if i+1 >= len(b) {
			out = append(out, b[i])
			break
		}
		out = append(out, b[i], b[i+1])
		i += 2
	}
	return out
}
