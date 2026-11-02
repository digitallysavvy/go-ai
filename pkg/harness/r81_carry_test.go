package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of harness/src/agent/harness-agent-tool-approval-continuation.test.ts
// and harness-agent-tool-result-continuation.test.ts (r81 carry), plus the
// pointer-part forms the core approval helpers accept.

func r81carryModelMessages(t *testing.T, uiJSON string) []types.Message {
	t.Helper()
	var ui []ai.UIMessage
	if err := json.Unmarshal([]byte(uiJSON), &ui); err != nil {
		t.Fatal(err)
	}
	messages, err := ai.ConvertToModelMessages(context.Background(), ui)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

type r81carryDecision struct {
	ApprovalID string
	Approved   bool
	Reason     string
}

func r81carryDecisions(c []types.ToolApprovalResponseContent) []r81carryDecision {
	out := []r81carryDecision{}
	for _, r := range c {
		out = append(out, r81carryDecision{r.ApprovalID, r.Approved, r.Reason})
	}
	return out
}

func TestR81carryApprovalContinuationsMixedDecisions(t *testing.T) {
	for _, providerExecuted := range []bool{false, true} {
		t.Run(fmt.Sprintf("providerExecuted=%v", providerExecuted), func(t *testing.T) {
			messages := r81carryModelMessages(t, fmt.Sprintf(`[{"id":"m1","role":"assistant","parts":[
				{"type":"tool-weather","toolCallId":"call-1","state":"approval-responded","input":{"city":"Paris"},"providerExecuted":%[1]v,"approval":{"id":"approval-1","approved":true}},
				{"type":"tool-weather","toolCallId":"call-2","state":"approval-responded","input":{"city":"Tokyo"},"providerExecuted":%[1]v,"approval":{"id":"approval-2","approved":false,"reason":"User denied the weather lookup."}}
			]}]`, providerExecuted))
			got, err := CollectToolApprovalContinuations(messages)
			if err != nil {
				t.Fatal(err)
			}
			want := []r81carryDecision{{"approval-1", true, ""}, {"approval-2", false, "User denied the weather lookup."}}
			if !reflect.DeepEqual(r81carryDecisions(got), want) {
				t.Fatalf("continuations = %+v, want %+v", r81carryDecisions(got), want)
			}
		})
	}
}

func TestR81carryApprovalContinuationsEveryApprovalDenied(t *testing.T) {
	messages := r81carryModelMessages(t, `[{"id":"m1","role":"assistant","parts":[
		{"type":"tool-weather","toolCallId":"call-1","state":"approval-responded","input":{"city":"Paris"},"approval":{"id":"approval-1","approved":false}}
	]}]`)
	got, err := CollectToolApprovalContinuations(messages)
	if err != nil {
		t.Fatal(err)
	}
	if want := []r81carryDecision{{"approval-1", false, ""}}; !reflect.DeepEqual(r81carryDecisions(got), want) {
		t.Fatalf("continuations = %+v, want %+v", r81carryDecisions(got), want)
	}
}

func TestR81carryApprovalContinuationsIgnoreCompletedResults(t *testing.T) {
	messages := r81carryModelMessages(t, `[{"id":"m1","role":"assistant","parts":[
		{"type":"tool-weather","toolCallId":"call-1","state":"output-available","input":{"city":"Paris"},"output":{"celsius":12},"approval":{"id":"approval-1","approved":true}},
		{"type":"tool-weather","toolCallId":"call-2","state":"output-denied","input":{"city":"Tokyo"},"approval":{"id":"approval-2","approved":false}}
	]}]`)
	got, err := CollectToolApprovalContinuations(messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("continuations = %+v, want none", got)
	}
}

func TestR81carryResultContinuationsTrailingToolMessageOnly(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "old", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"celsius": 18}}},
		}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "continue"}}},
	}
	if got := CollectToolResultContinuations(messages); len(got) != 0 {
		t.Fatalf("continuations = %+v, want none", got)
	}
}

// Messages built by hand may hold pointer parts; ai.CollectToolApprovals and
// the core approval resume accept them, so the harness continuations must
// too (they were silently dropped, and the paused turn never resumed).
func TestR81carryContinuationsAcceptPointerParts(t *testing.T) {
	result := &types.ToolResultContent{ToolCallID: "call-2", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "done"}}
	messages := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			&types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather"},
			&types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1"},
			&types.ToolCallContent{ToolCallID: "call-2", ToolName: "weather"},
			&types.ToolApprovalRequestContent{ApprovalID: "approval-2", ToolCallID: "call-2"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			&types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
			&types.ToolApprovalResponseContent{ApprovalID: "approval-2", Approved: true},
			result,
		}},
	}
	got, err := CollectToolApprovalContinuations(messages)
	if err != nil {
		t.Fatal(err)
	}
	if want := []r81carryDecision{{"approval-1", true, ""}}; !reflect.DeepEqual(r81carryDecisions(got), want) {
		t.Fatalf("approval continuations = %+v, want %+v", r81carryDecisions(got), want)
	}
	results := CollectToolResultContinuations(messages)
	if len(results) != 1 || results[0].ToolCallID != "call-2" {
		t.Fatalf("result continuations = %+v, want call-2", results)
	}
	// A nil pointer part is skipped, not dereferenced.
	messages[1].Content = append(messages[1].Content, (*types.ToolResultContent)(nil), (*types.ToolApprovalResponseContent)(nil))
	if _, err := CollectToolApprovalContinuations(messages); err != nil {
		t.Fatal(err)
	}
	if got := CollectToolResultContinuations(messages); len(got) != 1 {
		t.Fatalf("result continuations = %+v, want 1", got)
	}
}
