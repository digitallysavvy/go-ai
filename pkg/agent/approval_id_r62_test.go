package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r62Counter() ai.IDGenerator {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("id-%d", n)
	}
}

func r62ApprovalIDs(content []types.ContentPart) (requests, responses []string) {
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			requests = append(requests, p.ApprovalID)
		case types.ToolApprovalResponseContent:
			responses = append(responses, p.ApprovalID)
		}
	}
	return requests, responses
}

func r62Tools(executed *int) []types.Tool {
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		*executed++
		return "ok", nil
	}
	return []types.Tool{
		{Name: "ask", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: types.ToolApprovalStatusUserApproval, Execute: exec},
		{Name: "auto", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: types.ToolApprovalStatusApproved, Execute: exec},
	}
}

// r62 (owner ruling b): the legacy loop names approvals with the run's ID
// generator, as TS ToolLoopAgent's generateText does (generateId() per
// applicable approval, after the response ID fallback), and matches
// GenerateText's IDs for the same run.
func TestR62GenerateAgentApprovalIDsUseGenerator(t *testing.T) {
	newModel := func() *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{
				{ID: "c1", ToolName: "ask", Arguments: map[string]interface{}{}},
				{ID: "c2", ToolName: "auto", Arguments: map[string]interface{}{}},
			}}, nil
		}}
	}
	executed := 0
	res, err := NewToolLoopAgent(AgentConfig{Model: newModel(), Tools: r62Tools(&executed)}).GenerateAgent(context.Background(), AgentGenerateOptions{
		Prompt: "go", Internal: &ai.InternalOptions{GenerateID: r62Counter()},
	})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: newModel(), Prompt: "go", Tools: r62Tools(&executed), Internal: &ai.InternalOptions{GenerateID: r62Counter()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || len(gen.Steps) != 1 {
		t.Fatalf("steps: agent %d, generate %d; want 1 each", len(res.Steps), len(gen.Steps))
	}
	if got, want := res.Steps[0].Response.ID, gen.Steps[0].Response.ID; got != want || got != "id-1" {
		t.Errorf("response ID = %q, GenerateText %q; want id-1", got, want)
	}
	requests, responses := r62ApprovalIDs(res.Steps[0].Content)
	wantRequests, wantResponses := r62ApprovalIDs(gen.Steps[0].Content)
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) || fmt.Sprint(requests) != "[id-2 id-3]" {
		t.Errorf("request IDs = %v, GenerateText %v; want [id-2 id-3]", requests, wantRequests)
	}
	if fmt.Sprint(responses) != fmt.Sprint(wantResponses) || fmt.Sprint(responses) != "[id-3]" {
		t.Errorf("response IDs = %v, GenerateText %v; want [id-3]", responses, wantResponses)
	}
}

// r62: a provider that repeats tool call IDs across steps still gets a
// distinct approval per call (the reason for the owner ruling).
func TestR62GenerateAgentRepeatedCallIDsGetDistinctApprovals(t *testing.T) {
	step := 0
	model := &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		step++
		if step > 2 {
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{
			{ID: "c1", ToolName: "auto", Arguments: map[string]interface{}{}},
		}}, nil
	}}
	executed := 0
	res, err := NewToolLoopAgent(AgentConfig{Model: model, Tools: r62Tools(&executed)}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range res.Steps {
		requests, _ := r62ApprovalIDs(s.Content)
		for _, id := range requests {
			if id == "c1" || seen[id] {
				t.Fatalf("approval ID %q repeats or is the call ID (steps %+v)", id, res.Steps)
			}
			seen[id] = true
		}
	}
	if len(seen) != 2 || executed != 2 {
		t.Fatalf("approvals %v, executed %d; want 2 and 2", seen, executed)
	}
}

// r62: an approval round trip on the generated ID, and an approval persisted
// by v0.5.2 (approval ID == tool call ID, signed on it) still resumes.
func TestR62GenerateAgentResumesGeneratedAndV052ApprovalIDs(t *testing.T) {
	secret := []byte("r62-secret")
	executed := 0
	tool := approvalSecretTestTool(&executed)
	agent := NewToolLoopAgent(AgentConfig{Model: approvalRequestingModel(), Tools: []types.Tool{tool}, ExperimentalToolApprovalSecret: secret})
	initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
	first, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Messages: initial})
	if err != nil {
		t.Fatal(err)
	}
	req := approvalRequest(t, first.Steps[0].ResponseMessages)
	if req.ApprovalID == "" || req.ApprovalID == req.ToolCallID {
		t.Fatalf("approval ID = %q, want a generated ID (call %q)", req.ApprovalID, req.ToolCallID)
	}

	resume := func(approvalID string, sign bool) []types.Message {
		history := append([]types.Message(nil), initial...)
		for _, msg := range first.Steps[0].ResponseMessages {
			content := make([]types.ContentPart, 0, len(msg.Content))
			for _, part := range msg.Content {
				if r, ok := part.(types.ToolApprovalRequestContent); ok {
					r.ApprovalID = approvalID
					if sign {
						// The tool call approvalRequestingModel makes.
						r.Signature, _ = ai.SignToolApproval(secret, approvalID, "call-1", "tool1", map[string]interface{}{"value": "x"})
					}
					part = r
				}
				content = append(content, part)
			}
			msg.Content = content
			history = append(history, msg)
		}
		return append(history, types.Message{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: approvalID, Approved: true},
		}})
	}

	if _, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Messages: resume(req.ApprovalID, false)}); err != nil {
		t.Fatal(err)
	}
	if executed != 1 {
		t.Fatalf("generated-ID approval executed %d times, want 1", executed)
	}
	// v0.5.2 history: the request and response carry the call ID.
	if _, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Messages: resume(req.ToolCallID, true)}); err != nil {
		t.Fatal(err)
	}
	if executed != 2 {
		t.Fatalf("v0.5.2 approval executed %d times in total, want 2", executed)
	}
}
