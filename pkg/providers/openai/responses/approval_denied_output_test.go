package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The execution-denied result the core builds for a denied provider-executed
// tool carries the approval id on the OUTPUT's providerOptions (TS
// output.providerOptions.openai.approvalId); the tool message then yields only
// the mcp_approval_response, not a function_call_output.
func TestApprovalDeniedOutputProviderOptionsSkipsResult(t *testing.T) {
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
