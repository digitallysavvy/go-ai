package workflow

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r74 gen->carry: after the ValidateToolResults approval-response exemption
// was dropped, a hand-built workflow history whose provider tool call lacks
// ProviderExecuted fails with MissingToolResultsError, as TS (a call is exempt
// only by providerExecuted on the call). A provider-executed tool definition
// still exempts its calls (Go-only name list).
func TestR74Integ_WorkflowValidateToolResultsApprovalResponseNoExemption(t *testing.T) {
	history := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "search"}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "a1", ToolCallID: "c1", Approved: true, ProviderExecuted: true}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "next"}}},
	}
	var missing *types.MissingToolResultsError
	if err := validateWorkflowToolResults(history, []types.Tool{{Name: "search"}}); !errors.As(err, &missing) || len(missing.ToolCallIDs) != 1 || missing.ToolCallIDs[0] != "c1" {
		t.Fatalf("err = %v, want MissingToolResultsError for c1", err)
	}
	if err := validateWorkflowToolResults(history, []types.Tool{{Name: "search", ProviderExecuted: true}}); err != nil {
		t.Fatalf("provider-executed tool definition: err = %v, want nil", err)
	}
}
