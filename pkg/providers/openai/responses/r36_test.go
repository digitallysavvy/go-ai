package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS processedApprovalIds is shared across every tool message of the prompt,
// so a repeated approval id in a later tool message is skipped.
func TestApprovalResponsesDeduplicatedAcrossToolMessages(t *testing.T) {
	approval := func(approved bool) types.Message {
		return types.Message{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: approved},
		}}
	}
	input := ConvertPromptToInput(types.Prompt{Messages: []types.Message{approval(true), approval(false)}}, "system")
	if len(input) != 1 {
		t.Fatalf("input len = %d, want 1 (second approval skipped): %#v", len(input), input)
	}
}
