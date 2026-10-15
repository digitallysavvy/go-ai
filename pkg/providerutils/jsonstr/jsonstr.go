// Package jsonstr serializes values like JavaScript's JSON.stringify: HTML
// characters (<, > and &) stay literal instead of being escaped as < etc.
// Use it for JSON text the model sees (tool-call arguments, tool results).
package jsonstr

import (
	"bytes"
	"encoding/json"
)

// Marshal is json.Marshal without HTML escaping and without a trailing newline.
func Marshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
