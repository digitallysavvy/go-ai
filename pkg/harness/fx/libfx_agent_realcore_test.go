package fx

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// HarnessAgent-level cases of TS fx-session-native.test.ts "parallel fx
// approvals", on the real core through harness.Agent.

func weatherTool(mu *sync.Mutex, calls *int, celsius int) types.Tool {
	return types.Tool{
		Name:       "weather",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []any{"city"}},
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			*calls++
			mu.Unlock()
			return map[string]any{"city": input["city"], "celsius": celsius}, nil
		},
	}
}

// countToolResultChunks drains a stream and counts its tool-result parts
// (TS: part.type === 'tool-result').
func countToolResultChunks(t *testing.T, r *ai.StreamTextResult) int {
	t.Helper()
	stream := r.FullStream()
	n := 0
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return n
		}
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if chunk.Type == provider.ChunkTypeToolResult {
			n++
		}
	}
}

func TestLibfxAgentSettlesAutomaticCustomDecisions(t *testing.T) {
	for _, decision := range []ai.ToolApprovalStatus{ai.ToolApprovalStatusApproved, ai.ToolApprovalStatusDenied} {
		t.Run(string(decision), func(t *testing.T) {
			h, gw, sb, _, _ := parallelContext(t, "custom")
			var mu sync.Mutex
			calls := 0
			a, err := harness.NewAgent(harness.AgentSettings{
				Harness:      h,
				UserTools:    map[string]types.Tool{"weather": weatherTool(&mu, &calls, 20)},
				ToolApproval: harness.ToolApprovalConfiguration{"weather": decision},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			session, err := a.CreateSession(ctx, harness.CreateSessionOptions{SandboxSession: sb})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Destroy(ctx) //nolint:errcheck
			result, err := a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Weather for both cities.", HarnessSession: session}})
			if err != nil {
				t.Fatal(err)
			}
			results := countToolResultChunks(t, result)
			if err := result.Err(); err != nil {
				t.Fatal(err)
			}
			want := 0
			if decision == ai.ToolApprovalStatusApproved {
				want = 2
			}
			mu.Lock()
			if calls != want {
				t.Errorf("executions = %d, want %d", calls, want)
			}
			mu.Unlock()
			if results != 2 {
				t.Errorf("tool results = %d", results)
			}
			if n := gw.postCount(); n != 2 {
				t.Errorf("model requests = %d", n)
			}
			if n := len(result.Steps()); n != 2 {
				t.Errorf("steps = %d", n)
			}
		})
	}
}

func TestLibfxAgentExposesBothCustomApprovals(t *testing.T) {
	h, gw, sb, _, _ := parallelContext(t, "custom")
	var mu sync.Mutex
	calls := 0
	a, err := harness.NewAgent(harness.AgentSettings{
		Harness:      h,
		UserTools:    map[string]types.Tool{"weather": weatherTool(&mu, &calls, 20)},
		ToolApproval: harness.ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	session, err := a.CreateSession(ctx, harness.CreateSessionOptions{SandboxSession: sb})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy(ctx) //nolint:errcheck
	first, err := a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Weather for Paris and Tokyo in parallel.", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if err := first.Err(); err != nil {
		t.Fatal(err)
	}
	var approvals []types.ToolApprovalRequestContent
	for _, part := range first.Content() {
		if r, ok := part.(types.ToolApprovalRequestContent); ok {
			approvals = append(approvals, r)
		}
	}
	var cities []any
	for _, ap := range approvals {
		cities = append(cities, ap.ToolCall.Arguments["city"])
	}
	mu.Lock()
	executions := calls
	mu.Unlock()
	if len(approvals) != 2 || cities[0] != "Paris" || cities[1] != "Tokyo" || executions != 0 || gw.postCount() != 1 {
		t.Fatalf("approvals %v cities %v executions %d requests %d", len(approvals), cities, executions, gw.postCount())
	}
	responses := make([]types.ToolApprovalResponseContent, len(approvals))
	for i, ap := range approvals {
		responses[i] = types.ToolApprovalResponseContent{ApprovalID: ap.ApprovalID, ToolCallID: ap.ToolCallID, Approved: true}
	}
	second, err := a.ContinueStream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{HarnessSession: session}}, responses, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := countToolResultChunks(t, second)
	if err := second.Err(); err != nil {
		t.Fatal(err)
	}
	for _, part := range second.Content() {
		if _, ok := part.(types.ToolApprovalRequestContent); ok {
			t.Error("continuation asked for approval again")
		}
	}
	mu.Lock()
	if calls != 2 {
		t.Errorf("executions = %d", calls)
	}
	mu.Unlock()
	if results != 2 || gw.postCount() != 2 || len(first.Steps()) != 1 || len(second.Steps()) != 1 {
		t.Errorf("results %d requests %d steps %d/%d", results, gw.postCount(), len(first.Steps()), len(second.Steps()))
	}
}
