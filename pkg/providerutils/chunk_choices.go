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
