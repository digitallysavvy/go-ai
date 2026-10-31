package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r74carryIntPtr(v int) *int { return &v }

// r74carryApprovedMessages builds a resume history with three approved calls
// to the same approval-requiring tool.
func r74carryApprovedMessages(ids ...string) []types.Message {
	assistant := types.Message{Role: types.RoleAssistant}
	tool := types.Message{Role: types.RoleTool}
	for _, id := range ids {
		assistant.Content = append(assistant.Content,
			types.ToolCallContent{ToolCallID: id, ToolName: "tool1", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-" + id, ToolCallID: id})
		tool.Content = append(tool.Content, types.ToolApprovalResponseContent{ApprovalID: "approval-" + id, Approved: true})
	}
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}},
		assistant,
		tool,
	}
}

// TS workflow-agent.ts runs resumed approved tools through mapWithConcurrency,
// which starts the calls in model order, so onToolExecutionStart fires in call
// order even when an earlier call's start callback is slow; the executions
// still overlap (call-1 blocks until call-3 runs, so serializing them fails).
func TestR74Carry_ApprovedToolStartCallbacksInCallOrder(t *testing.T) {
	for _, concurrency := range []*int{nil, r74carryIntPtr(3)} {
		call3Running := make(chan struct{})
		var mu sync.Mutex
		var starts []string
		model := &wfMockModel{}
		agent, err := NewWorkflowAgent(WorkflowAgent{
			Model:               model,
			ToolCallConcurrency: concurrency,
			Tools: []types.Tool{{
				Name:         "tool1",
				ToolApproval: true,
				Execute: func(_ context.Context, _ map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					switch opts.ToolCallID {
					case "call-1":
						select {
						case <-call3Running:
						case <-time.After(5 * time.Second):
							return nil, errors.New("call-3 never ran while call-1 was executing")
						}
					case "call-3":
						close(call3Running)
					}
					return "ok", nil
				},
			}},
			OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
				if e.ToolCall.ID == "call-1" {
					time.Sleep(30 * time.Millisecond)
				}
				mu.Lock()
				defer mu.Unlock()
				starts = append(starts, e.ToolCall.ID)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: r74carryApprovedMessages("call-1", "call-2", "call-3")})
		if err != nil {
			t.Fatal(err)
		}
		_ = stream.Steps()
		mu.Lock()
		got := strings.Join(starts, ",")
		mu.Unlock()
		if got != "call-1,call-2,call-3" {
			t.Errorf("concurrency=%v: start order = %s, want call-1,call-2,call-3", concurrency, got)
		}
		for _, id := range []string{"call-1", "call-2", "call-3"} {
			tr := lastToolResultInPrompt(t, model, id)
			if tr.Output == nil || tr.Output.Type == types.ToolResultOutputErrorText {
				t.Errorf("concurrency=%v: %s result = %+v, want success", concurrency, id, tr.Output)
			}
		}
	}
}

// A call that returns before reaching its start callbacks (here: input
// revalidation fails) must still release the next call's start gate.
func TestR74Carry_ApprovedToolStartGateReleasedOnEarlyReturn(t *testing.T) {
	var mu sync.Mutex
	var starts []string
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name:         "tool1",
			ToolApproval: true,
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"n": map[string]interface{}{"type": "number"}},
				"required":   []interface{}{"n"},
			},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
		OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			starts = append(starts, e.ToolCall.ID)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := r74carryApprovedMessages("call-1", "call-2")
	// call-1 has invalid input (revalidation fails before any start callback).
	msgs[1].Content[0] = types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}}
	msgs[1].Content[2] = types.ToolCallContent{ToolCallID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{"n": 1}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs})
		if err != nil {
			t.Error(err)
			return
		}
		_ = stream.Steps()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("resume deadlocked waiting on a start gate")
	}
	mu.Lock()
	got := strings.Join(starts, ",")
	mu.Unlock()
	if got != "call-2" {
		t.Errorf("starts = %s, want call-2", got)
	}
	if tr := lastToolResultInPrompt(t, model, "call-1"); tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText {
		t.Errorf("call-1 result = %+v, want error-text", tr.Output)
	}
}
