package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertToOpenAIResponsesInput warns "system messages are removed for this
// model" for every system message in remove mode, including the system prompt.
func TestR37_SystemPromptRemovedWarns(t *testing.T) {
	prompt := types.Prompt{System: "be brief", Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}}
	input, warnings, err := ConvertPromptToInputWithOptions(prompt, "remove", ConvertOptions{Store: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 1 {
		t.Errorf("input = %#v", input)
	}
	if len(warnings) != 1 || warnings[0].Type != "other" || warnings[0].Message != "system messages are removed for this model" {
		t.Errorf("warnings = %#v", warnings)
	}
	// No system prompt, no warning.
	prompt.System = ""
	_, warnings, _ = ConvertPromptToInputWithOptions(prompt, "remove", ConvertOptions{Store: true})
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings %#v", warnings)
	}
}

// The execution-denied result the core builds for a denied provider-executed
// tool carries the approval id on the OUTPUT's providerOptions (TS
// output.providerOptions.openai.approvalId); the tool message then yields only
// the mcp_approval_response, not a function_call_output.
func TestR37_ApprovalDeniedOutputProviderOptionsSkipsResult(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{
		Role: types.RoleTool,
		Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: false},
			types.ToolResultContent{
				ToolCallID: "call-1",
				ToolName:   "mcp.tool",
				Output: &types.ToolResultOutput{
					Type:            types.ToolResultOutputExecutionDenied,
					Reason:          "blocked",
					ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"approvalId": "approval-1"}},
				},
			},
		},
	}}}
	input, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 1 {
		t.Fatalf("input = %#v, want only the approval response", input)
	}
	if _, ok := input[0].(MCPApprovalResponse); !ok {
		t.Fatalf("input[0] = %T", input[0])
	}
}

// webSearchOutputSchema validates sources at the top level, so an invalid
// source rejects the whole output for every action type (TS then falls back to
// the generic handling instead of rebuilding a web_search_call).
func TestR37_WebSearchInvalidSourcesRejectedForEveryAction(t *testing.T) {
	for _, action := range []map[string]interface{}{
		{"type": "search", "query": "q"},
		{"type": "openPage", "url": "https://example.com"},
		{"type": "findInPage", "url": "https://example.com", "pattern": "p"},
	} {
		part := types.ToolResultContent{
			ToolCallID: "ws_1",
			ToolName:   "web_search",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{
				"action":  action,
				"sources": []interface{}{map[string]interface{}{"type": "url"}},
			}},
		}
		if got := convertWebSearchToolResultOutput(part, "ws_1"); got != nil {
			t.Errorf("%v: got %#v, want nil", action["type"], got)
		}
		part.Output.Value = map[string]interface{}{"action": action}
		if got := convertWebSearchToolResultOutput(part, "ws_1"); got == nil {
			t.Errorf("%v: valid output was rejected", action["type"])
		}
	}
}
