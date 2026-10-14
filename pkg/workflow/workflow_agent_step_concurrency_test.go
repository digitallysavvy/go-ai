package workflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// twoToolCallModel answers the first call (generate or stream) with two tool
// calls in one step and every later call with text.
type twoToolCallModel struct {
	wfMockModel
	mu    sync.Mutex
	turns int
}

func (m *twoToolCallModel) next() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns++
	return m.turns == 1
}

var twoToolCalls = []types.ToolCall{
	{ID: "1", ToolName: "first", Arguments: map[string]interface{}{}},
	{ID: "2", ToolName: "second", Arguments: map[string]interface{}{}},
}

func (m *twoToolCallModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.next() {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: twoToolCalls}, nil
	}
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func (m *twoToolCallModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	if m.next() {
		first, second := twoToolCalls[0], twoToolCalls[1]
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &first},
			{Type: provider.ChunkTypeToolCall, ToolCall: &second},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "done"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

// TS #22135: WorkflowAgent's toolCallConcurrency (per-call value over the
// agent default) also bounds the model step loop's tool execution, not only
// approved-call resume. Unset runs a step's calls concurrently.
// (Go: makeAgent passes it to the ToolLoopAgent.)
func TestWorkflowStepLoopHonorsToolCallConcurrency(t *testing.T) {
	// run reports whether the two tool calls overlapped. Overlap is proven by
	// a rendezvous: each call waits (bounded) while the other is running.
	run := func(t *testing.T, agentLimit, callLimit *int, stream bool) bool {
		t.Helper()
		var mu sync.Mutex
		started, active := 0, 0
		bothActive := make(chan struct{})
		overlapped := false
		mkTool := func(name string) types.Tool {
			return types.Tool{
				Name:       name,
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					mu.Lock()
					started++
					active++
					if active == 2 {
						overlapped = true
						close(bothActive)
					}
					mu.Unlock()
					select {
					case <-bothActive:
					case <-time.After(300 * time.Millisecond):
					}
					mu.Lock()
					active--
					mu.Unlock()
					return name, nil
				},
			}
		}
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model:               &twoToolCallModel{},
			Tools:               []types.Tool{mkTool("first"), mkTool("second")},
			ToolCallConcurrency: agentLimit,
		})
		if err != nil {
			t.Fatal(err)
		}
		if stream {
			res, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go", ToolCallConcurrency: callLimit})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := res.ReadAll(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "go", ToolCallConcurrency: callLimit}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if started != 2 {
			t.Fatalf("tool executions = %d, want 2", started)
		}
		return overlapped
	}
	one := 1
	// Stream: the agent step loop honors the option (unset = concurrent).
	if !run(t, nil, nil, true) {
		t.Fatal("stream, unset: tool calls should run concurrently")
	}
	if run(t, &one, nil, true) {
		t.Fatal("stream, agent ToolCallConcurrency 1: tool calls overlapped")
	}
	if run(t, nil, &one, true) {
		t.Fatal("stream, per-call ToolCallConcurrency 1: tool calls overlapped")
	}
	// Generate goes through ToolLoopAgent.GenerateAgent's legacy loop, which
	// still executes a step's tools sequentially (tracked gap, see
	// state TRACKER); the bound is passed through so it applies once that
	// loop honors ToolCallConcurrency. Only the sequential bound is checked.
	if run(t, &one, nil, false) || run(t, nil, &one, false) {
		t.Fatal("generate, ToolCallConcurrency 1: tool calls overlapped")
	}
}
