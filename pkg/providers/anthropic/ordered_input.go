package anthropic

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// inputText is JSON.stringify(part.input) for a response block input, keeping
// the wire key order.
func inputText(raw json.RawMessage, fallback map[string]interface{}) string {
	return jsonorder.Text(raw, fallback)
}

// UnmarshalJSON decodes the block and keeps the wire form of its input.
func (c *anthropicContent) UnmarshalJSON(b []byte) error {
	type plain anthropicContent
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*c = anthropicContent(p)
	var aux struct {
		Input     json.RawMessage `json:"input"`
		Signature json.RawMessage `json:"signature"`
	}
	if json.Unmarshal(b, &aux) == nil {
		if len(aux.Input) > 0 && string(aux.Input) != "null" {
			c.RawInput = aux.Input
		}
		// TS `part.signature != null`: an empty string still counts.
		c.HasSignature = len(aux.Signature) > 0 && string(aux.Signature) != "null"
	}
	return nil
}
