package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r63Counter() IDGenerator {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("id-%d", n)
	}
}

// r63ContentShape describes step content as TS convertLanguageModelContent
// orders it: part type, tool call and approval ID.
func r63ContentShape(content []types.ContentPart) string {
	var out []string
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolCallContent:
			out = append(out, "call:"+p.ToolCallID)
		case types.ToolApprovalRequestContent:
			out = append(out, fmt.Sprintf("request:%s:%s:auto=%v", p.ToolCallID, p.ApprovalID, p.IsAutomatic))
		case types.ToolApprovalResponseContent:
			out = append(out, fmt.Sprintf("response:%s:%s:approved=%v", p.ToolCallID, p.ApprovalID, p.Approved))
		case types.ToolResultContent:
			out = append(out, "result:"+p.ToolCallID)
		case types.ToolErrorContent:
			out = append(out, "error:"+p.ToolCallID)
		default:
			out = append(out, fmt.Sprintf("%T", part))
		}
	}
	return strings.Join(out, " ")
}

// TS generateText and streamText record the automatic approval of a local
// call whose tool has no execute (or that a finish reason keeps from running)
// as tool-approval-request (isAutomatic) and tool-approval-response parts,
// with an approval ID from generateId(), and no tool output: the call stays
// unanswered, so the run stops after the step.
func TestR63ApprovedCallThatDoesNotRunKeepsItsApprovalParts(t *testing.T) {
	for _, tc := range []struct {
		reason types.FinishReason
		want   string
	}{
		// c1's tool has no Execute; c2's runs.
		{types.FinishReasonToolCalls, "call:c1 call:c2 request:c1:id-2:auto=true request:c2:id-3:auto=true response:c1:id-2:approved=true response:c2:id-3:approved=true result:c2"},
		// A length finish runs no tool.
		{types.FinishReasonLength, "call:c1 call:c2 request:c1:id-2:auto=true request:c2:id-3:auto=true response:c1:id-2:approved=true response:c2:id-3:approved=true"},
	} {
		calls := []types.ToolCall{
			{ID: "c1", ToolName: "client", Arguments: map[string]interface{}{}},
			{ID: "c2", ToolName: "local", Arguments: map[string]interface{}{}},
		}
		modelCalls := 0
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				modelCalls++
				return &types.GenerateResult{ToolCalls: calls, FinishReason: tc.reason}, nil
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				modelCalls++
				chunks := []provider.StreamChunk{}
				for i := range calls {
					c := calls[i]
					chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &c})
				}
				return testutil.NewMockTextStream(append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: tc.reason})), nil
			},
		}
		tools := []types.Tool{
			{Name: "client", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: types.ToolApprovalStatusApproved},
			{Name: "local", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: types.ToolApprovalStatusApproved,
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "ok", nil
				}},
		}

		gen, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "go", Tools: tools, StopWhen: []StopCondition{StepCountIs(3)},
			Internal: &InternalOptions{GenerateID: r63Counter()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if modelCalls != 1 || len(gen.Steps) != 1 {
			t.Fatalf("%s: GenerateText made %d model calls, %d steps; want 1 (the unanswered call ends the run)", tc.reason, modelCalls, len(gen.Steps))
		}
		if got := r63ContentShape(gen.Steps[0].Content); got != tc.want {
			t.Errorf("%s: GenerateText content\n got %s\nwant %s", tc.reason, got, tc.want)
		}
		for _, r := range gen.Steps[0].ToolResults {
			if r.ToolCallID == "c1" {
				t.Errorf("%s: GenerateText step tool results include the call that did not run: %+v", tc.reason, r)
			}
		}
		// The tool message carries the approval responses and c2's output
		// only (TS toResponseMessages).
		var toolParts []string
		for _, m := range gen.Steps[0].ResponseMessages {
			if m.Role == types.RoleTool {
				toolParts = append(toolParts, r63ContentShape(m.Content))
			}
		}
		// (A tool message's approval response carries no tool call ID.)
		wantTool := "response::id-2:approved=true response::id-3:approved=true result:c2"
		if tc.reason == types.FinishReasonLength {
			wantTool = "response::id-2:approved=true response::id-3:approved=true"
		}
		if got := strings.Join(toolParts, " | "); got != wantTool {
			t.Errorf("%s: GenerateText tool message\n got %s\nwant %s", tc.reason, got, wantTool)
		}

		modelCalls = 0
		st, err := StreamText(context.Background(), StreamTextOptions{
			Model: model, Prompt: "go", Tools: tools, StopWhen: []StopCondition{StepCountIs(3)},
			Internal: &InternalOptions{GenerateID: r63Counter()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.ReadAll(); err != nil {
			t.Fatal(err)
		}
		steps := st.Steps()
		if modelCalls != 1 || len(steps) != 1 {
			t.Fatalf("%s: StreamText made %d model calls, %d steps; want 1", tc.reason, modelCalls, len(steps))
		}
		// StreamText keeps stream order: each call's approval parts follow it
		// (TS executeToolsFromStream), the outputs come after.
		wantStream := "call:c1 request:c1:id-2:auto=true response:c1:id-2:approved=true call:c2 request:c2:id-3:auto=true response:c2:id-3:approved=true"
		if tc.reason == types.FinishReasonToolCalls {
			wantStream += " result:c2"
		}
		if got := r63ContentShape(steps[0].Content); got != wantStream {
			t.Errorf("%s: StreamText content\n got %s\nwant %s", tc.reason, got, wantStream)
		}
	}
}
