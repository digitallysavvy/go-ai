package providerutils

import (
	"encoding/json"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertToResponseMessage converts accumulated assistant content and tool
// calls into a response message suitable for adding to conversation history.
//
// It mirrors the TypeScript SDK's response-message conversion behavior for
// tool calls: invalid raw JSON inputs are skipped, and nil tool-call arguments
// default to an empty JSON object.
func ConvertToResponseMessage(toolCalls []types.ToolCall, content []types.ContentPart) types.Message {
	msg := types.Message{
		Role:    types.RoleAssistant,
		Content: filterResponseContent(content),
	}
	if len(toolCalls) == 0 {
		return msg
	}
	msg.ToolCalls = make([]types.ToolCall, 0, len(toolCalls))
	for _, call := range toolCalls {
		normalized, ok := normalizeResponseToolCall(call)
		if !ok {
			continue
		}
		msg.ToolCalls = append(msg.ToolCalls, normalized)
	}
	return msg
}

// ConvertToResponseMessages returns the assistant response message followed by
// a single tool message when local tool results are present.
func ConvertToResponseMessages(toolCalls []types.ToolCall, content []types.ContentPart, toolResults []types.ToolResult) []types.Message {
	messages := make([]types.Message, 0, 2)
	assistantMessage := ConvertToResponseMessage(toolCalls, content)
	if responseMessageHasContent(assistantMessage) {
		messages = append(messages, assistantMessage)
	}
	if len(toolResults) == 0 {
		return messages
	}
	parts := make([]types.ContentPart, 0, len(toolResults))
	for _, result := range toolResults {
		if result.ProviderExecuted {
			continue
		}
		parts = append(parts, types.ToolResultContent{
			ToolCallID: result.ToolCallID,
			ToolName:   result.ToolName,
			Result:     result.Result,
		})
	}
	if len(parts) > 0 {
		messages = append(messages, types.Message{
			Role:    types.RoleTool,
			Content: parts,
		})
	}
	return messages
}

func responseMessageHasContent(msg types.Message) bool {
	return len(msg.Content) > 0 || len(msg.ToolCalls) > 0
}

func normalizeResponseToolCall(call types.ToolCall) (types.ToolCall, bool) {
	if call.RawArguments != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(call.RawArguments), &parsed); err != nil {
			log.Printf("go-ai: skipping tool call %q with invalid JSON input: %v", call.ID, err)
			return types.ToolCall{}, false
		}
		if parsed == nil {
			parsed = map[string]interface{}{}
		}
		call.Arguments = parsed
	}
	if call.Arguments == nil {
		call.Arguments = map[string]interface{}{}
	}
	return call, true
}

func filterResponseContent(content []types.ContentPart) []types.ContentPart {
	if len(content) == 0 {
		return nil
	}
	filtered := make([]types.ContentPart, 0, len(content))
	for _, part := range content {
		switch p := part.(type) {
		case types.TextContent:
			if p.Text == "" {
				continue
			}
		case *types.TextContent:
			if p == nil || p.Text == "" {
				continue
			}
		case types.SourceContent, *types.SourceContent:
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered
}
