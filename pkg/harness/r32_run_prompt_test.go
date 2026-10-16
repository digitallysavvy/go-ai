package harness

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS throws these as `Harness '<id>' ... .` (capital H, trailing period).
func TestRunPromptProtocolErrorsMatchTSText(t *testing.T) {
	d := &turnDriver{in: runPromptInput{
		Harness:              &mockHarnessAdapter{id: "h"},
		BuiltinToolFiltering: &BuiltinToolFiltering{Mode: BuiltinToolFilteringDeny, ToolNames: []string{"bash"}},
	}}
	if _, err := d.handleApprovalRequest(&ToolApprovalRequestPart{ApprovalID: "a", ToolCallID: "c"}); err == nil ||
		err.Error() != "Harness 'h' emitted approval request 'a' for unknown tool call 'c'." {
		t.Fatalf("unknown call: %v", err)
	}
	d.toolCallsByID = map[string]types.ToolCall{"c": {ID: "c", ToolName: "bash"}}
	if _, err := d.handleApprovalRequest(&ToolApprovalRequestPart{ApprovalID: "a", ToolCallID: "c"}); err == nil ||
		err.Error() != "Harness 'h' emitted a built-in tool approval request but does not support approval responses." {
		t.Fatalf("no submitter: %v", err)
	}
	d.activeToolSet = map[string]struct{}{"t": {}}
	if _, err := d.handleHostToolCall(&ToolCallPart{ToolCallID: "x", ToolName: "t"}); err == nil ||
		err.Error() != "Harness 'h' could not validate host tool 't'." {
		t.Fatalf("validate: %v", err)
	}
}

// TS enqueueHostToolOutcome emits a tool-error with no `dynamic` flag, even for
// the invalid-input message (only the first-pass invalid call sets it).
func TestHostOutcomeChunkSetsNoDynamicFlag(t *testing.T) {
	d := &turnDriver{in: runPromptInput{Harness: &mockHarnessAdapter{id: "h"}}}
	c := d.hostOutcomeChunk("c", "t", types.ToolCall{ID: "c", ToolName: "t"}, hostToolOutcome{err: errors.New(invalidToolInputMessage)})
	if c.ToolResult == nil || c.ToolResult.Error == nil || c.ToolResult.Dynamic {
		t.Fatalf("got %+v", c.ToolResult)
	}
}
