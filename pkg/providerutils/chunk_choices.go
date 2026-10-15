package providerutils

import (
	"encoding/json"
	"errors"
	"fmt"
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

// ChatChunkShape selects the zod requirements of a provider's streamed
// chat-completion chunk schema beyond the always-required `choices` array.
type ChatChunkShape struct {
	// ChoiceTextRequired: choices[].text is a required string (the legacy
	// completions chunk schema); delta members are then not checked.
	ChoiceTextRequired bool
	// DeltaRequired: choices[].delta must be an object (mistral, alibaba; the
	// others declare it nullish).
	DeltaRequired bool
	// ToolCallFunctionOptional: delta.tool_calls[].function may be absent
	// (alibaba); otherwise it must be an object.
	ToolCallFunctionOptional bool
}

// ValidateChatChunk mirrors the structural part of the TS zod chunk schemas for
// a streamed chat-completion frame: `choices` is an array of objects, and the
// per-shape required choice/delta/tool-call members are present with the right
// JSON kind. The numeric `index` members that some TS schemas require are not
// enforced (real servers always send them; being lenient there is harmless).
// A non-nil error is the equivalent of TS's safeParse failure.
func ValidateChatChunk(data []byte, shape ChatChunkShape) error {
	var top struct {
		Choices json.RawMessage `json:"choices"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	if len(top.Choices) == 0 || top.Choices[0] != '[' {
		return errors.New("choices: Invalid input: expected array")
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(top.Choices, &choices); err != nil {
		return err
	}
	for i, rawChoice := range choices {
		var choice map[string]json.RawMessage
		if err := json.Unmarshal(rawChoice, &choice); err != nil || choice == nil {
			return fmt.Errorf("choices[%d]: Invalid input: expected object", i)
		}
		if shape.ChoiceTextRequired {
			if t := choice["text"]; len(t) == 0 || t[0] != '"' {
				return fmt.Errorf("choices[%d].text: Invalid input: expected string", i)
			}
			continue
		}
		rawDelta, hasDelta := choice["delta"]
		if !hasDelta || string(rawDelta) == "null" {
			if shape.DeltaRequired {
				return fmt.Errorf("choices[%d].delta: Invalid input: expected object", i)
			}
			continue
		}
		var delta map[string]json.RawMessage
		if err := json.Unmarshal(rawDelta, &delta); err != nil || delta == nil {
			return fmt.Errorf("choices[%d].delta: Invalid input: expected object", i)
		}
		rawCalls, ok := delta["tool_calls"]
		if !ok || string(rawCalls) == "null" {
			continue
		}
		var calls []json.RawMessage
		if err := json.Unmarshal(rawCalls, &calls); err != nil {
			return fmt.Errorf("choices[%d].delta.tool_calls: Invalid input: expected array", i)
		}
		for j, rawCall := range calls {
			var call map[string]json.RawMessage
			if err := json.Unmarshal(rawCall, &call); err != nil || call == nil {
				return fmt.Errorf("choices[%d].delta.tool_calls[%d]: Invalid input: expected object", i, j)
			}
			fn, hasFn := call["function"]
			if !hasFn || string(fn) == "null" {
				if shape.ToolCallFunctionOptional && (!hasFn || string(fn) == "null") {
					continue
				}
				return fmt.Errorf("choices[%d].delta.tool_calls[%d].function: Invalid input: expected object", i, j)
			}
			if fn[0] != '{' {
				return fmt.Errorf("choices[%d].delta.tool_calls[%d].function: Invalid input: expected object", i, j)
			}
		}
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
