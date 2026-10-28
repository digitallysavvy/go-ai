package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r56CallModel records each model call's x-h/x-pc headers; with failFirst
// the first call fails with a retryable provider error.
type r56CallModel struct {
	scriptedModel
	failFirst bool
	mu        sync.Mutex
	headers   []string
	attempts  int
}

func (m *r56CallModel) note(o *provider.GenerateOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts++
	if m.failFirst {
		yes := true
		return &providererrors.ProviderError{Message: "overloaded", StatusCode: 503, Retryable: &yes}
	}
	m.headers = append(m.headers, o.Headers["x-h"]+"/"+o.Headers["x-pc"])
	return nil
}

func (m *r56CallModel) DoGenerate(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	if err := m.note(o); err != nil {
		return nil, err
	}
	return m.scriptedModel.DoGenerate(ctx, o)
}

func (m *r56CallModel) DoStream(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
	if err := m.note(o); err != nil {
		return nil, err
	}
	return m.scriptedModel.DoStream(ctx, o)
}

func r56Run(t *testing.T, w *WorkflowAgent, stream bool) error {
	t.Helper()
	if stream {
		res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go"})
		if err != nil {
			return err
		}
		_ = res.ConsumeStream()
		return res.Err()
	}
	_, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "go"})
	return err
}

func r56Tool() []types.Tool {
	return []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "r", nil
	}}}
}

// TS WorkflowAgent's prepareStep headers reach the step's model call and
// carry over to later steps (mergePrepareStepGenerationSettings, spread into
// doStreamStep's call options). Go's stream path dropped them: core
// PrepareStepOptions has no Headers.
func TestWorkflowPrepareStepHeadersReachModelCall(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			m := &r56CallModel{scriptedModel: scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}}}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: m, Tools: r56Tool(),
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					if o.StepNumber == 0 {
						o.Headers = map[string]string{"x-h": "1"}
					}
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
			if len(m.headers) != 2 || m.headers[0] != "1/" || m.headers[1] != "1/" {
				t.Fatalf("model call headers = %q, want [1/ 1/]", m.headers)
			}
		})
	}
}

// A PrepareStep hook starts from PrepareCall's headers, so returning them
// unchanged keeps them on every step.
func TestWorkflowPrepareStepKeepsPrepareCallHeaders(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			m := &r56CallModel{scriptedModel: scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}}}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: m, Tools: r56Tool(),
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.Headers = map[string]string{"x-pc": "1"}
					return o, nil
				},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
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
			if len(m.headers) != 2 || m.headers[0] != "/1" || m.headers[1] != "/1" {
				t.Fatalf("model call headers = %q, want [/1 /1]", m.headers)
			}
		})
	}
}

// TS prepareStep's maxRetries is the step's retry count; a negative one is
// rejected by prepareRetries.
func TestWorkflowPrepareStepMaxRetries(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			m := &r56CallModel{failFirst: true}
			zero := 0
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: m,
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
			if m.attempts != 1 {
				t.Fatalf("model attempts = %d, want 1 (PrepareStep maxRetries 0)", m.attempts)
			}
		})
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream]+"/negative", func(t *testing.T) {
			m := &r56CallModel{}
			neg := -1
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: m,
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.MaxRetries = &neg
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			err = r56Run(t, w, stream)
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) || invalid.Field != "maxRetries" {
				t.Fatalf("err = %v, want maxRetries InvalidArgumentError", err)
			}
		})
	}
}

// r56ToolsModel records the tool names sent on each model call.
type r56ToolsModel struct {
	scriptedModel
	mu    sync.Mutex
	tools []string
}

func (m *r56ToolsModel) note(o *provider.GenerateOptions) {
	var names []string
	for _, t := range o.Tools {
		names = append(names, t.Name)
	}
	m.mu.Lock()
	m.tools = append(m.tools, strings.Join(names, ","))
	m.mu.Unlock()
}

func (m *r56ToolsModel) DoGenerate(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.note(o)
	return m.scriptedModel.DoGenerate(ctx, o)
}

func (m *r56ToolsModel) DoStream(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
	m.note(o)
	return m.scriptedModel.DoStream(ctx, o)
}

// TS filters the agent's tools by the call's active tools (options.activeTools
// ?? this.activeTools, after prepareCall) and hands that set to the iterator,
// which filters it again by prepareStep's activeTools: a step can narrow the
// call's active tools but not widen them. Go's Generate path filtered the full
// tool set by the step's list, and its hooks did not see the agent-level
// ActiveTools.
func TestWorkflowPrepareStepActiveToolsNarrowCallActiveTools(t *testing.T) {
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "r", nil
	}
	for _, agentLevel := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			name := map[bool]string{false: "generate", true: "stream"}[stream] + map[bool]string{false: "/call", true: "/agent"}[agentLevel]
			t.Run(name, func(t *testing.T) {
				m := &r56ToolsModel{scriptedModel: scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "a", Arguments: map[string]interface{}{}}}}}
				var mu sync.Mutex
				var seenCall, seenStep []string
				cfg := WorkflowAgent{
					Model: m,
					Tools: []types.Tool{{Name: "a", Execute: exec}, {Name: "b", Execute: exec}, {Name: "c", Execute: exec}},
					PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
						mu.Lock()
						seenCall = append(seenCall, strings.Join(o.ActiveTools, ","))
						mu.Unlock()
						return o, nil
					},
					PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
						mu.Lock()
						seenStep = append(seenStep, strings.Join(o.ActiveTools, ","))
						mu.Unlock()
						o.ActiveTools = []string{"a", "b"}
						return o, nil
					},
				}
				var call []string
				if agentLevel {
					cfg.ActiveTools = []string{"a"}
				} else {
					call = []string{"a"}
				}
				w, err := NewWorkflowAgent(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if stream {
					res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go", ActiveTools: call})
					if err != nil {
						t.Fatal(err)
					}
					if err := res.ConsumeStream(); err != nil {
						t.Fatal(err)
					}
				} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "go", ActiveTools: call}); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				defer mu.Unlock()
				m.mu.Lock()
				defer m.mu.Unlock()
				if have := strings.Join(m.tools, " | "); have != "a | a" {
					t.Fatalf("model tools = %q, want %q", have, "a | a")
				}
				if have := strings.Join(seenCall, " | "); have != "a" {
					t.Fatalf("PrepareCall ActiveTools = %q, want %q", have, "a")
				}
				if len(seenStep) == 0 || seenStep[0] != "a" {
					t.Fatalf("step 0 PrepareStep ActiveTools = %q, want a first", seenStep)
				}
			})
		}
	}
}

// r56FailModel answers step 0 with a tool call (or fails it when failAt is
// 0) and fails the model call of step failAt.
type r56FailModel struct {
	wfMockModel
	mu     sync.Mutex
	n      int
	failAt int
}

func (m *r56FailModel) DoStream(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	n := m.n
	m.n++
	m.mu.Unlock()
	if n == m.failAt {
		return nil, errors.New("boom")
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	}), nil
}

func (m *r56FailModel) DoGenerate(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.mu.Lock()
	n := m.n
	m.n++
	m.mu.Unlock()
	if n == m.failAt {
		return nil, errors.New("boom")
	}
	return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}}, nil
}

// TS WorkflowAgent's stream mode calls onEnd for every run that was not
// aborted (`mode === 'stream' || !hasEncounteredError`), after onError, with
// the steps completed so far; generate fires it only on success. Go's stream
// fired no OnEnd when a model call (or a PrepareStep hook) failed.
func TestWorkflowStreamOnEndAfterFailure(t *testing.T) {
	cases := []struct {
		name        string
		failAt      int
		hookErr     bool
		stream      bool
		wantEnds    int
		wantSteps   int
		wantFinish  types.FinishReason
		wantOrdered string
	}{
		{name: "stream/first step fails", failAt: 0, stream: true, wantEnds: 1, wantSteps: 0, wantFinish: types.FinishReasonOther, wantOrdered: "err end"},
		{name: "stream/second step fails", failAt: 1, stream: true, wantEnds: 1, wantSteps: 1, wantFinish: types.FinishReasonToolCalls, wantOrdered: "err end"},
		{name: "stream/PrepareStep fails", failAt: -1, hookErr: true, stream: true, wantEnds: 1, wantSteps: 0, wantFinish: types.FinishReasonOther, wantOrdered: "err end"},
		{name: "generate/second step fails", failAt: 1, wantEnds: 0, wantOrdered: "err"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var order []string
			var ends []ai.OnFinishEvent
			cfg := WorkflowAgent{
				Model: &r56FailModel{failAt: tc.failAt}, Tools: r56Tool(),
				OnEnd: func(_ context.Context, e ai.OnFinishEvent) {
					mu.Lock()
					order = append(order, "end")
					ends = append(ends, e)
					mu.Unlock()
				},
				OnError: func(context.Context, error) { mu.Lock(); order = append(order, "err"); mu.Unlock() },
			}
			if tc.hookErr {
				cfg.PrepareStep = func(context.Context, LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					return LanguageModelCallOptions{}, errors.New("hook")
				}
			}
			w, err := NewWorkflowAgent(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := r56Run(t, w, tc.stream); err == nil {
				t.Fatal("want the run to fail")
			}
			mu.Lock()
			defer mu.Unlock()
			if have := strings.Join(order, " "); have != tc.wantOrdered {
				t.Fatalf("callbacks = %q, want %q", have, tc.wantOrdered)
			}
			if len(ends) != tc.wantEnds {
				t.Fatalf("OnEnd calls = %d, want %d", len(ends), tc.wantEnds)
			}
			if tc.wantEnds == 0 {
				return
			}
			if e := ends[0]; len(e.Steps) != tc.wantSteps || e.FinishReason != tc.wantFinish {
				t.Fatalf("OnEnd steps = %d finish = %q, want %d %q", len(e.Steps), e.FinishReason, tc.wantSteps, tc.wantFinish)
			}
		})
	}
}

// An aborted stream fires OnAbort and no OnEnd (TS notifyEnd returns early).
func TestWorkflowStreamNoOnEndAfterAbort(t *testing.T) {
	var mu sync.Mutex
	var ends, aborts int
	ctx, cancel := context.WithCancel(context.Background())
	m := &r56FailModel{failAt: -1}
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: m,
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel()
			return "r", nil
		}}},
		OnEnd:   func(context.Context, ai.OnFinishEvent) { mu.Lock(); ends++; mu.Unlock() },
		OnAbort: func(context.Context, []types.StepResult) { mu.Lock(); aborts++; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "go"})
	if err == nil {
		_ = res.ConsumeStream()
	}
	mu.Lock()
	defer mu.Unlock()
	if ends != 0 || aborts == 0 {
		t.Fatalf("OnEnd = %d, OnAbort = %d; want 0 and at least 1", ends, aborts)
	}
}

// A hook that edits the Headers, ProviderOptions or ToolsContext map it was
// given in place must not change the agent's maps: they are shared by every run of the agent, and concurrent
// runs would race on the map.
func TestWorkflowHookHeadersEditDoesNotLeakToAgent(t *testing.T) {
	for _, hook := range []string{"PrepareCall", "PrepareStep"} {
		for _, stream := range []bool{false, true} {
			t.Run(hook+"/"+map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
				edit := func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					if o.Headers != nil {
						o.Headers["x-h"] = "edited"
					}
					if o.ProviderOptions != nil {
						o.ProviderOptions["edited"] = true
					}
					if o.ToolsContext != nil {
						o.ToolsContext["edited"] = true
					}
					return o, nil
				}
				cfg := WorkflowAgent{
					Model: &r56CallModel{}, Headers: map[string]string{"x-pc": "base"},
					ProviderOptions: map[string]interface{}{"p": map[string]interface{}{}},
					ToolsContext:    map[string]interface{}{"t": "tc"},
				}
				if hook == "PrepareCall" {
					cfg.PrepareCall = edit
				} else {
					cfg.PrepareStep = edit
				}
				w, err := NewWorkflowAgent(cfg)
				if err != nil {
					t.Fatal(err)
				}
				var wg sync.WaitGroup
				for i := 0; i < 4; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_ = r56Run(t, w, stream)
					}()
				}
				wg.Wait()
				if _, ok := w.Headers["x-h"]; ok || len(w.Headers) != 1 {
					t.Fatalf("agent Headers = %v, want unchanged", w.Headers)
				}
				if len(w.ProviderOptions) != 1 || len(w.ToolsContext) != 1 {
					t.Fatalf("agent ProviderOptions = %v, ToolsContext = %v, want unchanged", w.ProviderOptions, w.ToolsContext)
				}
			})
		}
	}
}
