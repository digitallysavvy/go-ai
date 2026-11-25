package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports of workflow/src/workflow-agent-concurrency.test.ts "continues queued
// calls after a tool error" and "does not start queued calls after abort"
// (r81 carry), for generate and stream.

var r81carryIDs = []string{"first", "second", "third"}

// r81carryThreeCallModel answers the first model call with three "action"
// tool calls and every later call with text.
type r81carryThreeCallModel struct {
	wfMockModel
	mu    sync.Mutex
	turns int
}

func (m *r81carryThreeCallModel) first() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns++
	return m.turns == 1
}

func r81carryCalls() []types.ToolCall {
	out := make([]types.ToolCall, len(r81carryIDs))
	for i, id := range r81carryIDs {
		out[i] = types.ToolCall{ID: id, ToolName: "action", Arguments: map[string]interface{}{"id": id}}
	}
	return out
}

func (m *r81carryThreeCallModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.first() {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: r81carryCalls()}, nil
	}
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func (m *r81carryThreeCallModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	if m.first() {
		var chunks []provider.StreamChunk
		for _, c := range r81carryCalls() {
			c := c
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &c})
		}
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})
		return testutil.NewMockTextStream(chunks), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "done"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

type r81carryStarted struct {
	mu  sync.Mutex
	ids []string
}

func (s *r81carryStarted) add(id string) {
	s.mu.Lock()
	s.ids = append(s.ids, id)
	s.mu.Unlock()
}

func (s *r81carryStarted) get() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...)
}

func r81carryRun(ctx context.Context, a *WorkflowAgent, stream bool, limit *int) error {
	if stream {
		res, err := a.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "Run tools.", ToolCallConcurrency: limit})
		if err != nil {
			return err
		}
		if _, err := res.ReadAll(); err != nil {
			return err
		}
		return res.Err()
	}
	_, err := a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "Run tools.", ToolCallConcurrency: limit})
	return err
}

func TestR81carryWorkflowQueuedCallsContinueAfterToolError(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			started := &r81carryStarted{}
			one := 1
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model:               &r81carryThreeCallModel{},
				StopWhen:            []ai.StopCondition{ai.IsStepCount(1)},
				ToolCallConcurrency: &one,
				Tools: []types.Tool{{
					Name:       "action",
					Parameters: map[string]interface{}{"type": "object"},
					Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
						id, _ := args["id"].(string)
						started.add(id)
						if id == "first" {
							return nil, errors.New("tool failed")
						}
						return id, nil
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := r81carryRun(context.Background(), a, stream, nil); err != nil {
				t.Fatal(err)
			}
			if got := started.get(); fmt.Sprint(got) != fmt.Sprint(r81carryIDs) {
				t.Fatalf("started = %v, want %v", got, r81carryIDs)
			}
		})
	}
}

func TestR81carryWorkflowQueuedCallsNotStartedAfterAbort(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := &r81carryStarted{}
			one := 1
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model:               &r81carryThreeCallModel{},
				ToolCallConcurrency: &one,
				Tools: []types.Tool{{
					Name:       "action",
					Parameters: map[string]interface{}{"type": "object"},
					Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
						id, _ := args["id"].(string)
						started.add(id)
						cancel()
						return id, nil
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := r81carryRun(ctx, a, stream, nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled (TS AbortError)", err)
			}
			if got := started.get(); fmt.Sprint(got) != "[first]" {
				t.Fatalf("started = %v, want [first]", got)
			}
		})
	}
}
