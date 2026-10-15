package bedrock

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// jsonStringify is JS JSON.stringify for one value (prompt.JSONStringify:
// compact, no HTML escaping of <, > and &). Model-visible JSON must not
// contain <. Values JSONStringify cannot encode fall back to json.Marshal.
func jsonStringify(v interface{}) string {
	if s, err := prompt.JSONStringify(v); err == nil {
		return s
	}
	b, _ := json.Marshal(v) //nolint:errcheck // fallback for unencodable values
	return string(b)
}
