package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
