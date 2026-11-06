package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS workflow-agent-approval.test.ts "forwards provider approval responses
// without local execution", in generate and stream mode: a provider's
// approval request is issued, the local tool of the same name never runs, and
// the resumed run forwards the approval response to the provider as is. TS
// WorkflowAgent adds no local execution-denied result for a provider-executed
// denial; Go's generate mode added one (generateText's resume ran again in
// the legacy loop).
func TestR61WorkflowForwardsProviderApprovalResponses(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		for _, approved := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/approved=%v", mode, approved), func(t *testing.T) {
				executed := 0
				m := &wfScriptModel{responses: []types.GenerateResult{
					{Content: []types.ContentPart{
						types.ToolCallContent{ToolCallID: "call-1", ToolName: "action", Input: `{"value":"requested"}`, ProviderExecuted: true},
						types.ToolApprovalRequestContent{ApprovalID: "provider-approval", ToolCallID: "call-1"},
					}, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "action", RawArguments: `{"value":"requested"}`, ProviderExecuted: true}}, FinishReason: types.FinishReasonToolCalls},
					{Content: []types.ContentPart{types.TextContent{Text: "Done"}}, FinishReason: types.FinishReasonStop},
				}}
				a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "action", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					executed++
					return "local", nil
				}}}})
				if err != nil {
					t.Fatal(err)
				}
				prompt := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Perform the action."}}}}
				run := func(messages []types.Message) ([]types.Message, []types.ContentPart) {
					t.Helper()
					var msgs []types.Message
					var content []types.ContentPart
					var steps []types.StepResult
					if mode == "generate" {
						res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages})
						if err != nil {
							t.Fatal(err)
						}
						msgs, steps = res.ResponseMessages, res.Steps
					} else {
						res, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: messages})
						if err != nil {
							t.Fatal(err)
						}
						if err := res.ConsumeStream(); err != nil {
							t.Fatal(err)
						}
						steps = res.Steps()
						for _, s := range steps {
							msgs = append(msgs, s.Response.Messages...)
						}
					}
					for _, s := range steps {
						content = append(content, s.Content...)
					}
					return msgs, content
				}
				issued, content := run(prompt)
				found := false
				for _, p := range content {
					if r, ok := p.(types.ToolApprovalRequestContent); ok && r.ApprovalID == "provider-approval" {
						found = true
					}
				}
				if !found {
					t.Fatalf("no provider approval request in %+v", content)
				}
				resume := append(append(append([]types.Message{}, prompt...), issued...), types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "provider-approval", Approved: approved}}})
				run(resume)
				if executed != 0 {
					t.Fatalf("local tool executed %d times", executed)
				}
				if len(m.opts) != 2 {
					t.Fatalf("model calls = %d, want 2", len(m.opts))
				}
				var forwarded bool
				for _, msg := range m.opts[1].Prompt.Messages {
					if msg.Role != types.RoleTool {
						continue
					}
					for _, p := range msg.Content {
						switch v := p.(type) {
						case types.ToolApprovalResponseContent:
							if v.ApprovalID == "provider-approval" && v.Approved == approved {
								forwarded = true
							}
						case types.ToolResultContent:
							t.Fatalf("local tool result in the resumed prompt: %+v", v)
						}
					}
				}
				if !forwarded {
					t.Fatalf("approval response not forwarded: %+v", m.opts[1].Prompt.Messages)
				}
			})
		}
	}
}
