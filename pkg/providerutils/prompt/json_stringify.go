package prompt

import (
	"bytes"
	"encoding/json"
)

// marshalJSONLikeJS marshals v the way JSON.stringify does for text that is
// shown to a model: <, > and & are not HTML-escaped (json.Marshal escapes them
// as \u003c, \u003e, \u0026, so a tool result containing HTML would reach the
// model as escape sequences) and the trailing newline json.Encoder adds is
// dropped.
func marshalJSONLikeJS(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// JSONStringify serializes v like JavaScript's JSON.stringify as far as Go
// allows (no HTML escaping of <, > and &, no trailing newline). Use it
// instead of json.Marshal when the JSON text is shown to a model, e.g. as a
// tool result or tool call arguments string. Object keys are still sorted
// for maps, as in encoding/json.
func JSONStringify(v interface{}) (string, error) {
	b, err := marshalJSONLikeJS(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
