// Parallel tool call expansion (row 6be0f51).
//
// Some OpenAI models emit multiple simultaneous tool calls as a single
// internal "parallel" function call whose arguments contain a `tool_uses`
// array of `{recipient_name: "functions.<name>", parameters}` entries,
// instead of one function_call item per tool. This file expands that
// wrapper into one ToolCall per nested recipient when every recipient names
// a declared client-side function tool, preserving the wrapper's identity
// via ParallelToolCallMetadata so results can be regrouped into a single
// function_call_output on replay (see convert.go).
package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const (
	parallelToolName    = "parallel"
	recipientNamePrefix = "functions."
	parallelToolCallKey = "parallelToolCall"
)

// ParallelToolCallMetadata preserves the original "parallel" wrapper call's
// identity so child results can be regrouped and sent back as one
// function_call_output when Responses API server-side state (conversation or
// previousResponseId) is used.
type ParallelToolCallMetadata struct {
	ItemID     string `json:"itemId"`
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Input      string `json:"input"`
	Index      int    `json:"index"`
	Count      int    `json:"count"`
}

// GetParallelToolCallMetadata extracts ParallelToolCallMetadata from a
// decoded tool-call or tool-result's providerMetadata.<provider>.
// parallelToolCall, if present and well-formed. metadata is expected in the
// shape produced by openAIResponsesToolCallMetadata / a tool-result's
// merged provider metadata: {<providerName>: {parallelToolCall: {...}}}.
func GetParallelToolCallMetadata(metadata map[string]interface{}, providerName string) (ParallelToolCallMetadata, bool) {
	openaiMeta, ok := metadata[providerName].(map[string]interface{})
	if !ok {
		return ParallelToolCallMetadata{}, false
	}
	raw, ok := openaiMeta[parallelToolCallKey]
	if !ok {
		return ParallelToolCallMetadata{}, false
	}
	// raw may already be a ParallelToolCallMetadata value (set in-process by
	// ExpandParallelToolCall) or a generic map[string]interface{} (decoded
	// from JSON on a later turn); round-trip through JSON to normalize.
	data, err := json.Marshal(raw)
	if err != nil {
		return ParallelToolCallMetadata{}, false
	}
	var m ParallelToolCallMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return ParallelToolCallMetadata{}, false
	}
	if m.ToolCallID == "" || m.ToolName == "" || m.Index < 0 || m.Count <= m.Index {
		return ParallelToolCallMetadata{}, false
	}
	return m, true
}

// isFunctionTool reports whether t is a plain client-executed function tool
// (as opposed to a provider-defined or provider-executed tool), mirroring
// TS's `tool.type === 'function'` filter for functionTools.
func isFunctionTool(t types.Tool) bool {
	return t.Type != types.ToolTypeProviderDefined && !t.ProviderExecuted
}

// IsUndeclaredParallelToolCall reports whether toolName is the internal
// "parallel" wrapper name, respecting a user's own tool actually named
// "parallel" (in which case it is never treated as the wrapper). Matching
// TS's isUndeclaredParallelToolCall, the collision check only considers
// plain client-side function tools -- tools may be the full, unfiltered
// request tool list; a provider-defined/built-in tool named "parallel"
// does not suppress wrapper expansion.
func IsUndeclaredParallelToolCall(toolName string, tools []types.Tool) bool {
	if toolName != parallelToolName {
		return false
	}
	for _, t := range tools {
		if isFunctionTool(t) && t.Name == parallelToolName {
			return false
		}
	}
	return true
}

// ExpandParallelToolCall expands the internal "parallel" tool call wrapper
// into one ToolCall per nested recipient. tools should be the request's
// declared function tools (see isFunctionTool). It returns ok=false --
// meaning the caller should treat toolCallID as an ordinary, unexpanded tool
// call -- whenever toolName isn't the undeclared "parallel" wrapper, the
// input isn't valid JSON, tool_uses is missing/empty, or any entry doesn't
// name a declared function tool.
func ExpandParallelToolCall(toolCallID, toolName, input, itemID string, tools []types.Tool, providerName string) ([]types.ToolCall, bool) {
	if !IsUndeclaredParallelToolCall(toolName, tools) {
		return nil, false
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, false
	}

	rawToolUses, ok := parsed["tool_uses"].([]interface{})
	if !ok || len(rawToolUses) == 0 {
		return nil, false
	}

	availableToolNames := map[string]bool{}
	for _, t := range tools {
		if isFunctionTool(t) {
			availableToolNames[t.Name] = true
		}
	}

	expanded := make([]types.ToolCall, 0, len(rawToolUses))
	for index, rawUse := range rawToolUses {
		use, ok := rawUse.(map[string]interface{})
		if !ok {
			return nil, false
		}
		recipientName, ok := use["recipient_name"].(string)
		if !ok || !strings.HasPrefix(recipientName, recipientNamePrefix) {
			return nil, false
		}
		parameters, ok := use["parameters"].(map[string]interface{})
		if !ok {
			return nil, false
		}
		childToolName := strings.TrimPrefix(recipientName, recipientNamePrefix)
		if childToolName == "" || !availableToolNames[childToolName] {
			return nil, false
		}
		childInput, _ := json.Marshal(parameters)
		expanded = append(expanded, types.ToolCall{
			ID:           fmt.Sprintf("%s_%d", toolCallID, index),
			ToolName:     childToolName,
			Arguments:    parameters,
			RawArguments: string(childInput),
			ProviderMetadata: map[string]interface{}{
				providerName: map[string]interface{}{
					parallelToolCallKey: ParallelToolCallMetadata{
						ItemID:     itemID,
						ToolCallID: toolCallID,
						ToolName:   toolName,
						Input:      input,
						Index:      index,
						Count:      len(rawToolUses),
					},
				},
			},
		})
	}
	return expanded, true
}
