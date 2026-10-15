package prompt

import "github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"

// JSONStringify serializes v like JavaScript's JSON.stringify as far as Go
// allows (no HTML escaping of <, > and &, no trailing newline). Use it
// instead of json.Marshal when the JSON text is shown to a model, e.g. as a
// tool result or tool call arguments string. Object keys are still sorted
// for maps, as in encoding/json. It is the string form of jsonstr.Marshal.
func JSONStringify(v interface{}) (string, error) {
	b, err := jsonstr.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
