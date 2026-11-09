package anthropic

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// inputText is JSON.stringify(part.input) for a response block input, keeping
// the wire key order.
func inputText(raw json.RawMessage, fallback map[string]interface{}) string {
	if len(raw) > 0 {
		// TS JSON.stringify(part.input) of an input that is not an object
		// (z.unknown()) gives that value's text.
		if v, ok := jsonorder.Parse(raw); ok {
			if _, isObject := v.(*jsonorder.Object); !isObject {
				return jsonorder.Stringify(v)
			}
		}
	}
	return jsonorder.Text(raw, fallback)
}

// UnmarshalJSON decodes the block and keeps the wire form of its input. An
// input that is not a JSON object (TS tool_use `input: z.unknown()`, null
// included) leaves Input nil and is kept in RawInput, so the block still
// decodes.
func (c *anthropicContent) UnmarshalJSON(b []byte) error {
	type plain anthropicContent
	var p struct {
		*plain
		Input     json.RawMessage `json:"input"`
		Signature json.RawMessage `json:"signature"`
	}
	p.plain = (*plain)(c)
	*c = anthropicContent{}
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	c.Signature = ""
	if len(p.Signature) > 0 && string(p.Signature) != "null" {
		if err := json.Unmarshal(p.Signature, &c.Signature); err != nil {
			return err
		}
		// TS `part.signature != null`: an empty string still counts.
		c.HasSignature = true
	}
	if len(p.Input) > 0 {
		// A null input is kept too: TS JSON.stringify(part.input) gives
		// "null" for it.
		c.RawInput = p.Input
		var obj map[string]interface{}
		if string(p.Input) != "null" && json.Unmarshal(p.Input, &obj) == nil {
			c.Input = obj
		}
	}
	return nil
}

// rawInputValue is the decoded block input (TS part.input): Input when the
// wire input is an object, else the decoded RawInput (nil when absent).
func (c *anthropicContent) rawInputValue() interface{} {
	if c.Input != nil {
		return c.Input
	}
	if len(c.RawInput) == 0 {
		return nil
	}
	var v interface{}
	if json.Unmarshal(c.RawInput, &v) != nil {
		return nil
	}
	return v
}

// nonNullRaw is raw unless it is the JSON literal null (TS `part.input ?? {}`).
func nonNullRaw(raw json.RawMessage) json.RawMessage {
	if string(raw) == "null" {
		return nil
	}
	return raw
}
