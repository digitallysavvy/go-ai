package anthropic

import (
	"encoding/json"
	"strings"
)

// jsonStringify is JS JSON.stringify for one value: compact and without Go's
// HTML escaping of <, > and & (model-visible JSON must not contain \u003c).
// Local copy; replace with prompt.JSONStringify once that is on the
// integration branch.
func jsonStringify(v interface{}) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		b, _ := json.Marshal(v) //nolint:errcheck // fallback for unencodable values
		return string(b)
	}
	return strings.TrimRight(buf.String(), "\n")
}
