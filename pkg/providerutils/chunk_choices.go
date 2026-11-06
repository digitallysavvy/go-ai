package providerutils

import (
	"encoding/json"
	"errors"
)

// RequireChunkChoices reports the zod failure of a streamed chat-completion
// frame that has no `choices` array (TS chunk schemas declare it required, so
// a frame such as {"error": {...}} is a parse failure there).
func RequireChunkChoices(data []byte) error {
	var probe struct {
		Choices json.RawMessage `json:"choices"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if len(probe.Choices) == 0 || probe.Choices[0] != '[' {
		return errors.New("choices: Invalid input: expected array")
	}
	return nil
}

// HasStringField reports whether data is a JSON object whose member `key` is a
// string (zod `z.string()` presence check for discriminator fields).
func HasStringField(data []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	v := m[key]
	return len(v) > 0 && v[0] == '"'
}
