package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS WorkflowAgent onEnd `messages` is the conversation the loop ran on: the
// converted prompt (instructions as a system message, the history after the
// approval resume stripped the resolved approval parts, then the resumed tool
// results) followed by each step's messages. Go started from the raw input
// messages, so a caller persisting onEnd messages kept the approval parts
// without their results and the next run executed the tool again.
func TestWorkflowOnEndMessagesStartFromResolvedConversation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var got []ai.OnFinishEvent
			executions := 0
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model:        &wfMockModel{},
				Instructions: "Be brief.",
				Tools: []types.Tool{{
					Name:         "getWeather",
					ToolApproval: true,
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						mu.Lock()
						executions++
						mu.Unlock()
						return "sunny", nil
					},
				}},
				OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); got = append(got, e); mu.Unlock() },
			})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: workflowApprovalMessages("")})
				if err != nil {
					t.Fatal(err)
				}
				_ = res.ConsumeStream()
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: workflowApprovalMessages("")}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if executions != 1 || len(got) != 1 {
				t.Fatalf("executions = %d, onEnd calls = %d, want 1/1", executions, len(got))
			}
			msgs := got[0].Messages
			want := 4
			for _, s := range got[0].Steps {
				want += len(s.Response.Messages)
			}
			if len(msgs) != want {
				t.Fatalf("messages = %d, want %d: %+v", len(msgs), want, msgs)
			}
			if msgs[0].Role != types.RoleSystem || len(msgs[0].Content) != 1 || msgs[0].Content[0].(types.TextContent).Text != "Be brief." {
				t.Fatalf("messages[0] = %+v, want the instructions as a system message", msgs[0])
			}
			if msgs[1].Role != types.RoleUser || msgs[2].Role != types.RoleAssistant || msgs[3].Role != types.RoleTool {
				t.Fatalf("roles = %s/%s/%s, want user/assistant/tool", msgs[1].Role, msgs[2].Role, msgs[3].Role)
			}
			for _, m := range msgs[:4] {
				for _, p := range m.Content {
					switch p.(type) {
					case types.ToolApprovalRequestContent, types.ToolApprovalResponseContent:
						t.Fatalf("resolved approval part %T still in onEnd messages", p)
					}
				}
			}
			if len(msgs[2].Content) != 1 {
				t.Fatalf("assistant content = %+v, want only the tool call", msgs[2].Content)
			}
			tr, ok := msgs[3].Content[0].(types.ToolResultContent)
			if !ok || tr.ToolCallID != "weather-1" || len(msgs[3].Content) != 1 {
				t.Fatalf("tool message = %+v, want the resumed weather-1 result", msgs[3].Content)
			}
		})
	}
}

func TestWorkflowConversationBaseCombinesToolMessages(t *testing.T) {
	a := types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "a"}}}
	b := types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "b"}}}
	out := workflowConversationBase("", nil, []types.Message{a, b}, "")
	if len(out) != 1 || len(out[0].Content) != 2 {
		t.Fatalf("out = %+v, want one tool message with both results", out)
	}
	if len(a.Content) != 1 {
		t.Fatalf("input message was modified: %+v", a)
	}
	sys := []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "x"}}, ProviderOptions: map[string]interface{}{"p": map[string]interface{}{"k": 1}}}}
	out = workflowConversationBase("x", sys, nil, "hi")
	if len(out) != 2 || out[0].ProviderOptions == nil || out[1].Role != types.RoleUser {
		t.Fatalf("out = %+v, want the instruction message (with its provider options) then the prompt", out)
	}
}
