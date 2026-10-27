package fx

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
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
	stream := r.Stream()
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

// TestLibfxAgentEchoesAutomaticResultPastPendingCall mirrors TS "echoes an
// automatic custom result past an earlier pending call", for allow-all and
// allow-edits, across a detach and resume.
func TestLibfxAgentEchoesAutomaticResultPastPendingCall(t *testing.T) {
	for _, mode := range []harness.PermissionMode{harness.PermissionModeAllowAll, harness.PermissionModeAllowEdits} {
		t.Run(string(mode), func(t *testing.T) {
			h, gw, sb, effects, effMu := parallelContext(t, "mixed")
			var mu sync.Mutex
			weatherCalls, automaticCalls := 0, 0
			automatic := weatherTool(&mu, &automaticCalls, 18)
			automatic.Name = "automatic_weather"
			a, err := harness.NewAgent(harness.AgentSettings{
				Harness:        h,
				PermissionMode: mode,
				UserTools:      map[string]types.Tool{"weather": weatherTool(&mu, &weatherCalls, 12), "automatic_weather": automatic},
				ToolApproval:   harness.ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
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
			first, err := a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Run the mixed batch in parallel.", HarnessSession: session}})
			if err != nil {
				t.Fatal(err)
			}
			var firstResults []string
			collectResults(t, first, &firstResults)
			if err := first.Err(); err != nil {
				t.Fatal(err)
			}
			var approvals []types.ToolApprovalRequestContent
			var names []string
			for _, part := range first.Content() {
				if r, ok := part.(types.ToolApprovalRequestContent); ok {
					approvals = append(approvals, r)
					names = append(names, r.ToolCall.ToolName)
				}
			}
			wantApprovals, wantFirst := "weather", "automatic_weather,bash"
			if mode == harness.PermissionModeAllowEdits {
				wantApprovals, wantFirst = "bash,weather", "automatic_weather"
			}
			if joinSorted(names) != wantApprovals || joinSorted(firstResults) != wantFirst {
				t.Fatalf("approvals %v, first results %v", names, firstResults)
			}
			mu.Lock()
			if weatherCalls != 0 || automaticCalls != 1 {
				t.Errorf("executions weather %d automatic %d", weatherCalls, automaticCalls)
			}
			mu.Unlock()
			effMu.Lock()
			wantEffects := 1
			if mode == harness.PermissionModeAllowEdits {
				wantEffects = 0
			}
			if len(*effects) != wantEffects {
				t.Errorf("effects %v", *effects)
			}
			effMu.Unlock()
			resumeFrom, err := session.Detach(ctx)
			if err != nil {
				t.Fatal(err)
			}
			resumed, err := a.CreateSession(ctx, harness.CreateSessionOptions{SessionID: session.SessionID(), SandboxSession: sb, ResumeFrom: resumeFrom})
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.Destroy(ctx) //nolint:errcheck
			responses := make([]types.ToolApprovalResponseContent, len(approvals))
			for i, ap := range approvals {
				responses[i] = types.ToolApprovalResponseContent{ApprovalID: ap.ApprovalID, ToolCallID: ap.ToolCallID, Approved: true}
			}
			second, err := a.ContinueStream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{HarnessSession: resumed}}, responses, nil)
			if err != nil {
				t.Fatal(err)
			}
			var secondResults []string
			collectResults(t, second, &secondResults)
			if err := second.Err(); err != nil {
				t.Fatal(err)
			}
			wantSecond := "weather"
			if mode == harness.PermissionModeAllowEdits {
				wantSecond = "bash,weather"
			}
			if joinSorted(secondResults) != wantSecond {
				t.Errorf("second results %v", secondResults)
			}
			effMu.Lock()
			if len(*effects) != 1 || (*effects)[0] != "printf MIXED > mixed.txt" {
				t.Errorf("effects %v", *effects)
			}
			effMu.Unlock()
			mu.Lock()
			if weatherCalls != 1 || automaticCalls != 1 {
				t.Errorf("executions weather %d automatic %d", weatherCalls, automaticCalls)
			}
			mu.Unlock()
			if n := gw.postCount(); n != 2 {
				t.Errorf("requests = %d", n)
			}
		})
	}
}

func collectResults(t *testing.T, r *ai.StreamTextResult, names *[]string) {
	t.Helper()
	stream := r.Stream()
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
			*names = append(*names, chunk.ToolResult.ToolName)
		}
	}
}

func joinSorted(values []string) string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return strings.Join(out, ",")
}
