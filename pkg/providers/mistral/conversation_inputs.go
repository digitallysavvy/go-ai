package mistral

import (
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// convertToMistralConversationInputs converts a prompt to Conversations API
// inputs and instructions, mirroring TS convertToMistralConversationInputs.
func convertToMistralConversationInputs(messages []types.Message, names conversationToolNames) (inputs []map[string]interface{}, instructions string, err error) {
	inputs = []map[string]interface{}{}
	var instructionParts []string
	executions := map[string]bool{}

	appendToolResult := func(tr types.ToolResultContent) {
		if executions[tr.ToolCallID] {
			inputs = append(inputs, map[string]interface{}{
				"type":         "function.result",
				"tool_call_id": tr.ToolCallID,
				"result":       executionResultString(tr),
			})
			return
		}
		inputs = append(inputs, map[string]interface{}{
			"type":         "function.result",
			"tool_call_id": tr.ToolCallID,
			"result":       mistralToolResultText(tr),
		})
	}

	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			var b strings.Builder
			for _, p := range msg.Content {
				if tc, ok := p.(types.TextContent); ok {
					b.WriteString(tc.Text)
				}
			}
			instructionParts = append(instructionParts, b.String())
		case types.RoleUser:
			converted, err := convertMistralUserMessage(msg)
			if err != nil {
				return nil, "", err
			}
			inputs = append(inputs, map[string]interface{}{
				"type":    "message.input",
				"role":    "user",
				"content": converted["content"],
			})
		case types.RoleAssistant:
			var content []map[string]interface{}
			flush := func() {
				if len(content) > 0 {
					inputs = append(inputs, map[string]interface{}{"type": "message.input", "role": "assistant", "content": content})
					content = nil
				}
			}
			addToolCall := func(id, name string, input string, providerExecuted bool, providerOpts map[string]interface{}) {
				flush()
				if providerExecuted {
					wireName := names.providerName(name)
					if mistralOpts, ok := providerOpts["mistral"].(map[string]interface{}); ok {
						if fn, ok := mistralOpts["function"].(string); ok {
							wireName = fn
						} else if n, ok := mistralOpts["name"].(string); ok {
							wireName = n
						}
					}
					args := input
					var parsed struct {
						Arguments *string `json:"arguments"`
					}
					if json.Unmarshal([]byte(input), &parsed) == nil && parsed.Arguments != nil {
						args = *parsed.Arguments
					}
					// Replay built-in executions as calls and results. Entry
					// IDs and tool.execution output entries cannot be sent as
					// inputs.
					inputs = append(inputs, map[string]interface{}{"type": "function.call", "tool_call_id": id, "name": wireName, "arguments": args})
					executions[id] = true
					return
				}
				inputs = append(inputs, map[string]interface{}{"type": "function.call", "tool_call_id": id, "name": name, "arguments": input})
			}
			seenCalls := map[string]bool{}
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					content = append(content, map[string]interface{}{"type": "text", "text": p.Text})
				case types.ReasoningContent:
					content = append(content, map[string]interface{}{
						"type":     "thinking",
						"thinking": []map[string]interface{}{{"type": "text", "text": p.Text}},
						"closed":   true,
					})
				case types.ToolCallContent:
					seenCalls[p.ToolCallID] = true
					opts := p.ProviderOptions
					if opts == nil && len(p.ProviderMetadata) > 0 {
						_ = json.Unmarshal(p.ProviderMetadata, &opts)
					}
					addToolCall(p.ToolCallID, p.ToolName, toolCallContentInput(p), p.ProviderExecuted, opts)
				case types.ToolResultContent:
					flush()
					appendToolResult(p)
				default:
					return nil, "", &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("assistant content type: %s", part.ContentType())}
				}
			}
			for _, tc := range msg.ToolCalls {
				if seenCalls[tc.ID] {
					continue
				}
				input := tc.RawArguments
				if input == "" {
					args := tc.Arguments
					if args == nil {
						args = map[string]interface{}{}
					}
					b, _ := jsonstr.Marshal(args)
					input = string(b)
				}
				var opts map[string]interface{}
				if tc.ProviderMetadata != nil {
					opts = tc.ProviderMetadata
				}
				addToolCall(tc.ID, tc.ToolName, input, tc.ProviderExecuted, opts)
			}
			flush()
		case types.RoleTool:
			for _, part := range msg.Content {
				if tr, ok := part.(types.ToolResultContent); ok {
					appendToolResult(tr)
				}
			}
		}
	}

	if len(instructionParts) > 0 {
		instructions = strings.Join(instructionParts, "\n\n")
	}
	return inputs, instructions, nil
}

func toolCallContentInput(p types.ToolCallContent) string {
	if p.Input != "" {
		return p.Input
	}
	args := p.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	b, _ := jsonstr.Marshal(args)
	return string(b)
}

// executionResultString extracts the `info` of a built-in tool execution
// result as the function.result payload (TS appendToolResult).
func executionResultString(tr types.ToolResultContent) string {
	var value interface{}
	if tr.Output != nil {
		if tr.Output.Type == types.ToolResultOutputJSON {
			value = tr.Output.Value
		}
	} else {
		value = tr.Result
	}
	var result interface{}
	if m, ok := toStringMap(value); ok {
		if info, ok := toStringMap(m["info"]); ok {
			result = info
			if r, has := info["result"]; has && r != nil {
				result = r
			}
		}
	}
	if s, ok := result.(string); ok {
		return s
	}
	b, _ := jsonstr.Marshal(result)
	return string(b)
}

func toStringMap(v interface{}) (map[string]interface{}, bool) {
	if v == nil {
		return nil, false
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m, true
	}
	b, err := jsonstr.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]interface{}
	if json.Unmarshal(b, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}
