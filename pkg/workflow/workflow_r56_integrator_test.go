package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r56IntStepModel answers the first call with a tool call (with usage) and
// fails every later call with a retryable provider error.
type r56IntStepModel struct {
	wfMockModel
	mu       sync.Mutex
	attempts int
}

func (m *r56IntStepModel) next() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts++
	if m.attempts == 1 {
		return 1, nil
	}
	yes := true
	return m.attempts, &providererrors.ProviderError{Message: "overloaded", StatusCode: 503, Retryable: &yes}
}

func (m *r56IntStepModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	if _, err := m.next(); err != nil {
		return nil, err
	}
	in, out := int64(3), int64(4)
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: &in, OutputTokens: &out}},
	}), nil
}

func (m *r56IntStepModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	if _, err := m.next(); err != nil {
		return nil, err
	}
	return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}}, nil
}

// r56 integrator: a PrepareStep maxRetries reaches a later step's model call
// too, not only the first (stream path, ai.InternalOptions.StepCallSettings).
func TestR56IntegratorPrepareStepMaxRetriesLaterStep(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			m := &r56IntStepModel{}
			zero := 0
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: m, Tools: r56Tool(),
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.MaxRetries = &zero
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := r56Run(t, w, stream); err == nil {
				t.Fatal("want the provider error")
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.attempts != 2 {
				t.Fatalf("model attempts = %d, want 2 (one per step, PrepareStep maxRetries 0)", m.attempts)
			}
		})
	}
}

// r56 integrator: the OnEnd of a failed stream run reports the completed
// steps' total usage (TS notifyEnd totalUsage).
func TestR56IntegratorStreamOnEndAfterFailureTotalUsage(t *testing.T) {
	zero := 0
	var mu sync.Mutex
	var ends []ai.OnFinishEvent
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &r56IntStepModel{}, Tools: r56Tool(), MaxRetries: &zero,
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r56Run(t, w, true); err == nil {
		t.Fatal("want the run to fail")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("OnEnd calls = %d, want 1", len(ends))
	}
	u := ends[0].TotalUsage
	if u.InputTokens == nil || *u.InputTokens != 3 || u.OutputTokens == nil || *u.OutputTokens != 4 {
		t.Fatalf("total usage = %#v, want 3 in / 4 out from the completed step", u)
	}
}

// r56 integrator: a PrepareCall that returns no ActiveTools keeps the call's
// (here the agent's) active tools (TS `prepared.activeTools !== undefined`),
// so a PrepareStep still cannot widen them.
func TestR56IntegratorPrepareCallNilActiveToolsKeepsCallActiveTools(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			m := &r56ToolsModel{}
			exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "r", nil
			}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model:       m,
				Tools:       []types.Tool{{Name: "a", Execute: exec}, {Name: "b", Execute: exec}},
				ActiveTools: []string{"a"},
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.ActiveTools = nil
					return o, nil
				},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.ActiveTools = []string{"a", "b"}
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := r56Run(t, w, stream); err != nil {
				t.Fatal(err)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if len(m.tools) == 0 || m.tools[0] != "a" {
				t.Fatalf("model tools = %q, want only a", m.tools)
			}
		})
	}
}
