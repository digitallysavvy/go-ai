package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// callerMetadataJSON is anthropicCallerMetadata as raw JSON, for content parts.
func callerMetadataJSON(caller map[string]interface{}) json.RawMessage {
	meta := anthropicCallerMetadata(caller)
	if meta == nil {
		return nil
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return nil
	}
	return b
}

// computerToolset20260801Members lists the computer toolset's member tools
// (TS computerToolset_20260801Members).
var computerToolset20260801Members = []string{
	"screenshot", "zoom", "left_click", "right_click", "middle_click", "double_click",
	"triple_click", "left_click_drag", "mouse_move", "left_mouse_down", "left_mouse_up",
	"cursor_position", "scroll", "type", "key", "hold_key", "wait",
}

// validateToolsetMember ports TS validateToolsetMember: a toolset member call
// is accepted only for the computer toolset declared in the prepared tools,
// for a known member whose config is not `enabled: false`.
func validateToolsetMember(toolsetName, memberName string, preparedTools []map[string]interface{}) error {
	var toolset map[string]interface{}
	for _, t := range preparedTools {
		if t["type"] == "computer_toolset_20260801" {
			toolset = t
			break
		}
	}
	known := false
	for _, m := range computerToolset20260801Members {
		if m == memberName {
			known = true
			break
		}
	}
	if toolsetName != "computer" || toolset == nil || !known || toolsetMemberDisabled(toolset, memberName) {
		return providererrors.NewInvalidResponseDataError(
			map[string]interface{}{"toolsetName": toolsetName, "memberName": memberName},
			fmt.Sprintf("Received an unavailable toolset member: %s.%s.", toolsetName, memberName))
	}
	return nil
}

// toolsetMemberDisabled is TS `toolset.configs?.[memberName]?.enabled === false`.
func toolsetMemberDisabled(toolset map[string]interface{}, memberName string) bool {
	configs, ok := toolset["configs"].(map[string]interface{})
	if !ok {
		return false
	}
	cfg, ok := configs[memberName].(map[string]interface{})
	if !ok {
		return false
	}
	enabled, ok := cfg["enabled"].(bool)
	return ok && !enabled
}

// toToolsetMemberInput ports TS toToolsetMemberInput: the member input must
// be a JSON object, and the member name the API selected is the action
// (`{...input, action: memberName}`: it overrides an `action` key in the
// input).
func toToolsetMemberInput(memberName string, input interface{}) (map[string]interface{}, error) {
	obj, ok := input.(map[string]interface{})
	if !ok {
		return nil, providererrors.NewInvalidResponseDataError(input, "Invalid toolset member input: expected an object.")
	}
	out := make(map[string]interface{}, len(obj)+1)
	for k, v := range obj {
		out[k] = v
	}
	out["action"] = memberName
	return out, nil
}

// toolsetMemberInputText is JSON.stringify({...input, action: memberName})
// in wire key order: `action` keeps the position of an input `action` key,
// and otherwise comes last.
func toolsetMemberInputText(raw json.RawMessage, input map[string]interface{}, memberName string) string {
	o := jsonorder.Input(raw, input)
	o.Set("action", memberName)
	return o.String()
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
		tc.RawArguments = jsonorder.Input(content.RawInput, input).WithLead("type", content.Name).String()
		tc.Dynamic = markCodeExecutionDynamic
	case "web_search", "code_execution", "web_fetch":
		args := input
		ordered := jsonorder.Input(content.RawInput, input)
		if content.Name == "code_execution" {
			_, hasCode := input["code"]
			_, hasType := input["type"]
			if hasCode && !hasType {
				args = map[string]interface{}{"type": "programmatic-tool-call"}
				for k, v := range input {
					args[k] = v
				}
				ordered = ordered.WithLead("type", "programmatic-tool-call")
			}
			tc.Dynamic = markCodeExecutionDynamic
		}
		tc.ToolName = mapAnthropicToolName(content.Name, toolNameMap)
		tc.Arguments = args
		tc.RawArguments = ordered.String()
		if content.Input == nil && len(content.RawInput) > 0 {
			// TS JSON.stringify(part.input) of a null input is "null".
			tc.RawArguments = inputText(content.RawInput, nil)
		}
	case "tool_search_tool_regex", "tool_search_tool_bm25", "advisor":
		tc.ToolName = mapAnthropicToolName(content.Name, toolNameMap)
		tc.Arguments = input
		tc.RawArguments = inputText(content.RawInput, input)
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

// toolCallContentOf is the ordered-content form of a ToolCall (what the core
// derives from result.ToolCalls when a provider leaves Content to it).
func toolCallContentOf(tc types.ToolCall) types.ToolCallContent {
	var meta json.RawMessage
	if len(tc.ProviderMetadata) > 0 {
		meta, _ = json.Marshal(tc.ProviderMetadata)
	}
	return types.ToolCallContent{
		ToolCallID:       tc.ID,
		ToolName:         tc.ToolName,
		Arguments:        tc.Arguments,
		Input:            tc.RawArguments,
		ProviderExecuted: tc.ProviderExecuted,
		ProviderMetadata: meta,
		Dynamic:          tc.Dynamic,
	}
}

// deferredToolResult is the TS tool-result shape for the provider-executed
// code execution, tool search and advisor result blocks.
type deferredToolResult struct {
	// ToolName is the SDK-side (custom) tool name.
	ToolName string
	Result   interface{}
	// ErrorCode is non-empty when TS marks the result isError.
	ErrorCode string
	IsError   bool
}

// convertDeferredToolResult maps a code_execution_tool_result,
// bash_code_execution_tool_result, text_editor_code_execution_tool_result,
// tool_search_tool_result or advisor_tool_result block to the tool-result TS
// emits for it (doGenerate content and doStream content_block_start share
// the same mapping). serverToolName is the provider tool name recorded from
// the matching server_tool_use ("" when unknown). ok is false for a block
// TS emits nothing for (an unrecognised code_execution_tool_result payload).
func convertDeferredToolResult(blockType string, raw json.RawMessage, toolNameMap map[string]string, serverToolName string) (deferredToolResult, bool) {
	var content map[string]interface{}
	_ = json.Unmarshal(raw, &content)
	str := func(k string) interface{} { return content[k] }
	orEmptyList := func(k string) interface{} {
		if v, ok := content[k]; ok && v != nil {
			return v
		}
		return []interface{}{}
	}
	switch blockType {
	case "code_execution_tool_result":
		name := mapAnthropicToolName("code_execution", toolNameMap)
		switch content["type"] {
		case "code_execution_result":
			return deferredToolResult{ToolName: name, Result: map[string]interface{}{
				"type": "code_execution_result", "stdout": str("stdout"), "stderr": str("stderr"),
				"return_code": str("return_code"), "content": orEmptyList("content"),
			}}, true
		case "encrypted_code_execution_result":
			return deferredToolResult{ToolName: name, Result: map[string]interface{}{
				"type": "encrypted_code_execution_result", "encrypted_stdout": str("encrypted_stdout"), "stderr": str("stderr"),
				"return_code": str("return_code"), "content": orEmptyList("content"),
			}}, true
		case "code_execution_tool_result_error":
			code, _ := content["error_code"].(string)
			return deferredToolResult{ToolName: name, IsError: true, ErrorCode: code, Result: map[string]interface{}{
				"type": "code_execution_tool_result_error", "errorCode": str("error_code"),
			}}, true
		}
		return deferredToolResult{}, false
	case "bash_code_execution_tool_result", "text_editor_code_execution_tool_result":
		var passthrough interface{}
		_ = json.Unmarshal(raw, &passthrough)
		return deferredToolResult{ToolName: mapAnthropicToolName("code_execution", toolNameMap), Result: passthrough}, true
	case "tool_search_tool_result":
		provider := serverToolName
		if provider == "" {
			// TS: prefer whichever tool-search variant the caller named.
			provider = "tool_search_tool_regex"
			if mapAnthropicToolName("tool_search_tool_bm25", toolNameMap) != "tool_search_tool_bm25" {
				provider = "tool_search_tool_bm25"
			}
		}
		name := mapAnthropicToolName(provider, toolNameMap)
		if content["type"] == "tool_search_tool_search_result" {
			refs := []interface{}{}
			if list, ok := content["tool_references"].([]interface{}); ok {
				for _, r := range list {
					ref, _ := r.(map[string]interface{})
					refs = append(refs, map[string]interface{}{"type": ref["type"], "toolName": ref["tool_name"]})
				}
			}
			return deferredToolResult{ToolName: name, Result: refs}, true
		}
		code, _ := content["error_code"].(string)
		return deferredToolResult{ToolName: name, IsError: true, ErrorCode: code, Result: map[string]interface{}{
			"type": "tool_search_tool_result_error", "errorCode": str("error_code"),
		}}, true
	case "advisor_tool_result":
		res, isErr := convertAdvisorResult(raw)
		code := ""
		if isErr {
			code, _ = res["errorCode"].(string)
		}
		return deferredToolResult{ToolName: mapAnthropicToolName("advisor", toolNameMap), Result: res, IsError: isErr, ErrorCode: code}, true
	}
	return deferredToolResult{}, false
}

// validateToolsetMemberCalls runs the TS doGenerate toolset member checks
// (validateToolsetMember, then toToolsetMemberInput) over the response
// content in order, against the prepared tools of the request.
func validateToolsetMemberCalls(content []anthropicContent, usesJSONResponseTool bool, preparedTools []map[string]interface{}) error {
	for i := range content {
		part := &content[i]
		if part.Type != "tool_use" || part.ToolsetName == "" || (usesJSONResponseTool && part.Name == "json") {
			continue
		}
		if err := validateToolsetMember(part.ToolsetName, part.Name, preparedTools); err != nil {
			return err
		}
		if _, err := toToolsetMemberInput(part.Name, part.rawInputValue()); err != nil {
			return err
		}
	}
	return nil
}
