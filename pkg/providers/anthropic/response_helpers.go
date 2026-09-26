package anthropic

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// anthropicCallerInfo maps a wire caller ({type, tool_id?}) to the metadata
// shape {type, toolId?} (TS getAnthropicCallerInfo).
func anthropicCallerInfo(caller map[string]interface{}) map[string]interface{} {
	if caller == nil {
		return nil
	}
	out := map[string]interface{}{"type": caller["type"]}
	if toolID, ok := caller["tool_id"]; ok {
		out["toolId"] = toolID
	}
	return out
}

// anthropicCallerMetadata returns {"anthropic": {"caller": ...}} or nil.
func anthropicCallerMetadata(caller map[string]interface{}) map[string]interface{} {
	info := anthropicCallerInfo(caller)
	if info == nil {
		return nil
	}
	return map[string]interface{}{"anthropic": map[string]interface{}{"caller": info}}
}

// toToolsetMemberInput injects the toolset member name as the action
// (TS toToolsetMemberInput).
func toToolsetMemberInput(memberName string, input map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{"action": memberName}
	for k, v := range input {
		out[k] = v
	}
	// the member name wins, like TS { action, ...input } except input keys
	// override; TS spreads input after action, so input keys win.
	if a, ok := input["action"]; ok {
		out["action"] = a
	}
	return out
}

// serverToolUseCall converts a server_tool_use block into a provider-executed
// tool call, mirroring the TS doGenerate server_tool_use branch.
func serverToolUseCall(content anthropicContent, toolNameMap map[string]string, markCodeExecutionDynamic bool) (types.ToolCall, bool) {
	input := content.Input
	if input == nil {
		input = map[string]interface{}{}
	}
	tc := types.ToolCall{
		ID:               content.ID,
		ProviderExecuted: true,
		ProviderMetadata: anthropicCallerMetadata(content.Caller),
	}
	switch content.Name {
	case "text_editor_code_execution", "bash_code_execution":
		args := map[string]interface{}{"type": content.Name}
		for k, v := range input {
			args[k] = v
		}
		tc.ToolName = mapAnthropicToolName("code_execution", toolNameMap)
		tc.Arguments = args
		tc.Dynamic = markCodeExecutionDynamic
	case "web_search", "code_execution", "web_fetch":
		args := input
		if content.Name == "code_execution" {
			_, hasCode := input["code"]
			_, hasType := input["type"]
			if hasCode && !hasType {
				args = map[string]interface{}{"type": "programmatic-tool-call"}
				for k, v := range input {
					args[k] = v
				}
			}
			tc.Dynamic = markCodeExecutionDynamic
		}
		tc.ToolName = mapAnthropicToolName(content.Name, toolNameMap)
		tc.Arguments = args
	case "tool_search_tool_regex", "tool_search_tool_bm25", "advisor":
		tc.ToolName = mapAnthropicToolName(content.Name, toolNameMap)
		tc.Arguments = input
	default:
		return types.ToolCall{}, false
	}
	return tc, true
}

// convertAdvisorResult maps an advisor_tool_result content payload to the
// camelCase output shape. The second return value reports an error result.
func convertAdvisorResult(raw json.RawMessage) (map[string]interface{}, bool) {
	var wire struct {
		Type             string  `json:"type"`
		Text             string  `json:"text"`
		EncryptedContent string  `json:"encrypted_content"`
		StopReason       *string `json:"stop_reason"`
		ErrorCode        string  `json:"error_code"`
	}
	_ = json.Unmarshal(raw, &wire)
	switch wire.Type {
	case "advisor_result":
		out := map[string]interface{}{"type": wire.Type, "text": wire.Text}
		if wire.StopReason != nil {
			out["stopReason"] = *wire.StopReason
		}
		return out, false
	case "advisor_redacted_result":
		out := map[string]interface{}{"type": wire.Type, "encryptedContent": wire.EncryptedContent}
		if wire.StopReason != nil {
			out["stopReason"] = *wire.StopReason
		}
		return out, false
	default:
		return map[string]interface{}{"type": "advisor_tool_result_error", "errorCode": wire.ErrorCode}, true
	}
}

// rawJSONValue decodes a raw JSON value, returning nil for absent or null.
func rawJSONValue(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
