package cohere

import (
	"encoding/json"
	"errors"
	"fmt"
)

// validateCohereChunk mirrors the TS cohereChatChunkSchema discriminated
// union: an unknown or missing `type`, or a member with a missing/ill-typed
// required field, fails the parse (TS then emits an error part and finishes
// with "error").
func validateCohereChunk(data []byte) error {
	var top map[string]interface{}
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	if top == nil {
		return errors.New("invalid input: expected object")
	}
	typ, _ := top["type"].(string)
	num := func(v interface{}) bool { _, ok := v.(float64); return ok }
	str := func(v interface{}) bool { _, ok := v.(string); return ok }
	obj := func(v interface{}) map[string]interface{} { m, _ := v.(map[string]interface{}); return m }
	// path walks nested objects and returns the value (nil when absent).
	path := func(v map[string]interface{}, keys ...string) interface{} {
		var cur interface{} = v
		for _, k := range keys {
			m := obj(cur)
			if m == nil {
				return nil
			}
			cur = m[k]
		}
		return cur
	}
	fail := func(field, want string) error {
		return fmt.Errorf("%s: Invalid input: expected %s (chunk type %q)", field, want, typ)
	}

	switch typ {
	case "citation-start", "citation-end", "tool-call-end":
		return nil
	case "content-start":
		if !num(top["index"]) {
			return fail("index", "number")
		}
		content := obj(path(top, "delta", "message", "content"))
		switch {
		case content == nil:
			return fail("delta.message.content", "object")
		case content["type"] == "text" && str(content["text"]):
		case content["type"] == "thinking" && str(content["thinking"]):
		default:
			return fail("delta.message.content", "text or thinking content")
		}
	case "content-delta":
		if !num(top["index"]) {
			return fail("index", "number")
		}
		content := obj(path(top, "delta", "message", "content"))
		if content == nil || (!str(content["text"]) && !str(content["thinking"])) {
			return fail("delta.message.content", "text or thinking")
		}
	case "content-end":
		if !num(top["index"]) {
			return fail("index", "number")
		}
	case "message-start":
		if id, ok := top["id"]; ok && id != nil && !str(id) {
			return fail("id", "string")
		}
	case "message-end":
		if !str(path(top, "delta", "finish_reason")) {
			return fail("delta.finish_reason", "string")
		}
		usage := obj(path(top, "delta", "usage"))
		if usage == nil {
			return fail("delta.usage", "object")
		}
		tokens := obj(usage["tokens"])
		if tokens == nil || !num(tokens["input_tokens"]) || !num(tokens["output_tokens"]) {
			return fail("delta.usage.tokens", "input_tokens/output_tokens numbers")
		}
		if billed, ok := usage["billed_units"]; ok && billed != nil {
			b := obj(billed)
			if b == nil || !num(b["input_tokens"]) || !num(b["output_tokens"]) {
				return fail("delta.usage.billed_units", "input_tokens/output_tokens numbers")
			}
		}
		if c, ok := usage["cached_tokens"]; ok && c != nil && !num(c) {
			return fail("delta.usage.cached_tokens", "number")
		}
	case "tool-plan-delta":
		if !str(path(top, "delta", "message", "tool_plan")) {
			return fail("delta.message.tool_plan", "string")
		}
	case "tool-call-start":
		call := obj(path(top, "delta", "message", "tool_calls"))
		if call == nil || !str(call["id"]) || call["type"] != "function" {
			return fail("delta.message.tool_calls", "function tool call")
		}
		fn := obj(call["function"])
		if fn == nil || !str(fn["name"]) || !str(fn["arguments"]) {
			return fail("delta.message.tool_calls.function", "name and arguments strings")
		}
	case "tool-call-delta":
		if !str(path(top, "delta", "message", "tool_calls", "function", "arguments")) {
			return fail("delta.message.tool_calls.function.arguments", "string")
		}
	default:
		return fmt.Errorf("type: Invalid discriminator value %v", top["type"])
	}
	return nil
}
