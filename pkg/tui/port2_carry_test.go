package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS terminal-renderer.ts prints JSON.stringify(part.input, null, 2): the
// approval prompt shows the input in the key order the model sent it.
func TestPort2carryApprovalInputKeepsModelKeyOrder(t *testing.T) {
	for _, tc := range []struct {
		name        string
		raw         string
		wantOrdered bool
	}{
		{"raw input matches", `{"zebra":1,"apple":2}`, true},
		{"raw input differs from the input", `{"zebra":9,"apple":2}`, false},
		{"no raw input", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var atPrompt string
			tt := portcarrytuiNew(nil, 60, 16, "")
			tt.renderer.input = &portcarrytuiKeys{keys: []string{"n"}, before: func(int) { atPrompt = tt.screen() }}
			_, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
				ToolCallID: "c1", ToolName: "shell",
				Input:    map[string]interface{}{"zebra": float64(1), "apple": float64(2)},
				RawInput: tc.raw,
			}, TerminalSessionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			z, a := strings.Index(atPrompt, `"zebra"`), strings.Index(atPrompt, `"apple"`)
			if z < 0 || a < 0 {
				t.Fatalf("input not shown:\n%s", atPrompt)
			}
			if (z < a) != tc.wantOrdered {
				t.Fatalf("zebra before apple = %v, want %v:\n%s", z < a, tc.wantOrdered, atPrompt)
			}
		})
	}
}

func TestPort2carryPendingApprovalRequestCarriesRawInput(t *testing.T) {
	call := types.ToolCall{ID: "c1", ToolName: "shell", Arguments: map[string]interface{}{"b": float64(1), "a": float64(2)}, RawArguments: `{"b":1,"a":2}`}
	requests := findPendingToolApprovalRequests([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolApprovalRequestContent{ApprovalID: "ap1", ToolCall: call, ToolCallID: "c1"},
	}}})
	if len(requests) != 1 || requests[0].RawInput != `{"b":1,"a":2}` {
		t.Fatalf("requests = %+v, want the raw input carried", requests)
	}
}
