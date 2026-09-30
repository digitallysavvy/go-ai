package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func workflowApprovalMessages(signature string) []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather?"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "weather-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-weather-1", ToolCallID: "weather-1", Signature: signature},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-weather-1", Approved: true},
		}},
	}
}

func lastToolResultInPrompt(t *testing.T, model *wfMockModel, toolCallID string) types.ToolResultContent {
	t.Helper()
	if len(model.opts) == 0 {
		t.Fatal("expected provider call")
	}
	for _, msg := range model.opts[0].Prompt.Messages {
		for _, part := range msg.Content {
			if tr, ok := part.(types.ToolResultContent); ok && tr.ToolCallID == toolCallID {
				return tr
			}
		}
	}
	t.Fatalf("no tool result for %s in %+v", toolCallID, model.opts[0].Prompt.Messages)
	return types.ToolResultContent{}
}

// TS 69d7128 / 11109ae: WorkflowAgent reuses the core validator, so a forged
// (unsigned or tampered) approval is reported to the model and never executed.
func TestWorkflowResumeRejectsForgedSignedApproval(t *testing.T) {
	secret := []byte("workflow-secret")
	validSig, err := ai.SignToolApproval(secret, "approval-weather-1", "weather-1", "getWeather", map[string]interface{}{"city": "London"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		signature string
		wantExec  int
		wantError string
	}{
		// TS only reports "missing signature" for a null/undefined
		// signature, which it can distinguish from an explicit empty
		// string; Go's plain string field cannot represent that
		// distinction, so an empty signature falls through to
		// verification and fails as "invalid signature" like TS reports
		// for an explicit empty string (see pkg/ai/validate_tool_approvals.go).
		{name: "missing", signature: "", wantError: "invalid signature"},
		{name: "forged", signature: "Zm9yZ2Vk", wantError: "invalid signature"},
		{name: "valid", signature: validSig, wantExec: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &wfMockModel{}
			executions := 0
			agent, err := NewWorkflowAgent(WorkflowAgent{
				Model:                          model,
				ExperimentalToolApprovalSecret: secret,
				Tools: []types.Tool{{
					Name:         "getWeather",
					ToolApproval: true,
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						executions++
						return "sunny", nil
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: workflowApprovalMessages(tc.signature)})
			if err != nil {
				t.Fatal(err)
			}
			_ = stream.Steps()
			if executions != tc.wantExec {
				t.Fatalf("executions = %d, want %d", executions, tc.wantExec)
			}
			tr := lastToolResultInPrompt(t, model, "weather-1")
			if tc.wantError == "" {
				if tr.Output != nil && tr.Output.Type == types.ToolResultOutputErrorText {
					t.Fatalf("valid approval produced error: %+v", tr.Output)
				}
				return
			}
			if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText || !strings.Contains(tr.Output.Value.(string), tc.wantError) {
				t.Fatalf("tool result = %+v, want error-text containing %q", tr.Output, tc.wantError)
			}
		})
	}
}

// TS 05672ad: failed approved tool executions stream as tool errors.
func TestWorkflowResumeStreamsFailedApprovedToolAsToolError(t *testing.T) {
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name:         "getWeather",
			ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, errors.New("weather service down")
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var chunks []provider.StreamChunk
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages: workflowApprovalMessages(""),
		OnChunk:  func(c provider.StreamChunk) { chunks = append(chunks, c) },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Steps()
	var got *types.ToolResult
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && c.ToolResult.ToolCallID == "weather-1" {
			got = c.ToolResult
		}
	}
	if got == nil || got.Error == nil || got.Error.Error() != "weather service down" || got.Result != nil {
		t.Fatalf("tool chunk = %+v, want tool error", got)
	}
	tr := lastToolResultInPrompt(t, model, "weather-1")
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText || tr.Output.Value != "weather service down" {
		t.Fatalf("prompt tool result = %+v", tr.Output)
	}
}

// TS 238aff0: approved tools receive the conversation messages and fire the
// tool execution start/end callbacks.
func TestWorkflowResumePassesMessagesAndFiresToolCallbacks(t *testing.T) {
	model := &wfMockModel{}
	var execMessages []types.Message
	var starts, ends []string
	var endErr error
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name:         "getWeather",
			ToolApproval: true,
			Execute: func(_ context.Context, _ map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				execMessages = opts.Messages
				return "sunny", nil
			},
		}},
		OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) { starts = append(starts, e.ToolCallID) },
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := workflowApprovalMessages("")
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages: messages,
		OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			ends = append(ends, e.ToolCallID)
			endErr = e.Error
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Steps()
	if len(execMessages) != len(messages) {
		t.Fatalf("execute messages = %d, want %d", len(execMessages), len(messages))
	}
	if len(starts) != 1 || starts[0] != "weather-1" || len(ends) != 1 || ends[0] != "weather-1" || endErr != nil {
		t.Fatalf("callbacks start=%v end=%v err=%v", starts, ends, endErr)
	}
}

func TestWorkflowResumeRejectsUnknownApprovalID(t *testing.T) {
	agent, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "nope", Approved: true}}},
	}})
	if !ai.IsInvalidToolApprovalError(err) {
		t.Fatalf("err = %v", err)
	}
}
