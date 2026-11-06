package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS generate-text/tool-approval-follow-up.test.ts (TS #22449): an
// approved tool call that is not the final message's approval is rejected with
// MissingToolResultsError before the provider is invoked when later user
// context follows it.
func TestApprovedToolContinuation_ValidatesMissingToolResults(t *testing.T) {
	for _, api := range []string{"generate", "stream"} {
		for _, sequence := range []string{"unresolved-follow-up", "immediate", "completed-follow-up"} {
			t.Run(api+"/"+sequence, func(t *testing.T) {
				var executed int32
				tool := types.Tool{
					Name:         "set_theme",
					Parameters:   map[string]interface{}{"type": "object"},
					ToolApproval: types.ToolApprovalStatusUserApproval,
					Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
						atomic.AddInt32(&executed, 1)
						return "saved", nil
					},
				}
				messages := []types.Message{
					{Role: types.RoleAssistant, Content: []types.ContentPart{
						types.ToolCallContent{ToolCallID: "call", ToolName: "set_theme", Arguments: map[string]interface{}{}},
						types.ToolApprovalRequestContent{ApprovalID: "approval", ToolCallID: "call"},
					}},
				}
				toolContent := []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "approval", Approved: true}}
				if sequence == "completed-follow-up" {
					toolContent = append(toolContent, types.ToolResultContent{
						ToolCallID: "call", ToolName: "set_theme",
						Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "saved"},
					})
				}
				messages = append(messages, types.Message{Role: types.RoleTool, Content: toolContent})
				if sequence != "immediate" {
					messages = append(messages, types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Reply with OK."}}})
				}
				shouldReject := sequence == "unresolved-follow-up"

				model := newRecordingModel("OK")
				var err error
				if api == "generate" {
					_, err = GenerateText(context.Background(), GenerateTextOptions{Model: model, Tools: []types.Tool{tool}, Messages: messages})
				} else {
					var result *StreamTextResult
					result, err = StreamText(context.Background(), StreamTextOptions{Model: model, Tools: []types.Tool{tool}, Messages: messages})
					if err == nil {
						collectStream(t, result)
						err = result.Err()
					}
				}

				model.mu.Lock()
				calls := len(model.prompts)
				model.mu.Unlock()
				if shouldReject {
					var missing *types.MissingToolResultsError
					if !errors.As(err, &missing) {
						t.Fatalf("err = %v, want MissingToolResultsError", err)
					}
					if len(missing.ToolCallIDs) != 1 || missing.ToolCallIDs[0] != "call" {
						t.Fatalf("ToolCallIDs = %v", missing.ToolCallIDs)
					}
					if missing.Error() == "" || calls != 0 {
						t.Fatalf("calls = %d, err = %v", calls, err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if calls != 1 {
						t.Fatalf("provider calls = %d, want 1", calls)
					}
				}
				want := int32(0)
				if sequence == "immediate" {
					want = 1
				}
				if got := atomic.LoadInt32(&executed); got != want {
					t.Fatalf("executed = %d, want %d", got, want)
				}
			})
		}
	}
}
