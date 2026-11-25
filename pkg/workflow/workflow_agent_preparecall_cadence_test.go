package workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS WorkflowAgent runs prepareCall once per invocation, before the loop,
// while prepareStep runs before every step.
func TestWorkflowAgentPrepareCallOncePrepareStepEveryStep(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var order []string
			var callN, stepN atomic.Int32
			model := &twoToolCallModel{}
			tools := []types.Tool{
				{Name: "first", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "a", nil
				}},
				{Name: "second", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "b", nil
				}},
			}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: model, Tools: tools,
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					callN.Add(1)
					order = append(order, "call")
					o.System = "from-prepare-call"
					return o, nil
				},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					stepN.Add(1)
					order = append(order, "step")
					if o.System != "from-prepare-call" {
						t.Errorf("step %d: PrepareStep saw system %q, want PrepareCall's value", o.StepNumber, o.System)
					}
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
				if err != nil {
					t.Fatal(err)
				}
				if err := res.ConsumeStream(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
				t.Fatal(err)
			}
			if callN.Load() != 1 {
				t.Errorf("PrepareCall ran %d times, want 1 (%v)", callN.Load(), order)
			}
			if stepN.Load() != 2 {
				t.Errorf("PrepareStep ran %d times, want 2 (%v)", stepN.Load(), order)
			}
			if len(order) > 0 && order[0] != "call" {
				t.Errorf("order = %v, PrepareCall must run first", order)
			}
		})
	}
}

// prepareCall's message and setting changes apply to every step, and the
// per-call prepareStep replaces (does not stack on) the constructor one.
func TestWorkflowAgentPrepareCallResultsPersistAcrossSteps(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			model := &recordingTwoToolModel{}
			noop := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "x", nil
			}
			tools := []types.Tool{
				{Name: "first", Parameters: map[string]interface{}{"type": "object"}, Execute: noop},
				{Name: "second", Parameters: map[string]interface{}{"type": "object"}, Execute: noop},
			}
			temp := 0.25
			ctorStep, callStep := 0, 0
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: model, Tools: tools,
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.Messages = append(o.Messages, types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "injected"}}})
					o.Temperature = &temp
					return o, nil
				},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					ctorStep++
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			perCall := func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				callStep++
				return o, nil
			}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", PrepareStep: perCall})
				if err != nil {
					t.Fatal(err)
				}
				if err := res.ConsumeStream(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", PrepareStep: perCall}); err != nil {
				t.Fatal(err)
			}
			if ctorStep != 0 || callStep != 2 {
				t.Errorf("constructor prepareStep ran %d times, per-call %d; want 0 and 2", ctorStep, callStep)
			}
			if len(model.seen()) < 2 {
				t.Fatalf("model saw %d calls", len(model.seen()))
			}
			for i, o := range model.seen() {
				found := false
				for _, m := range o.Prompt.Messages {
					for _, p := range m.Content {
						if tc, ok := p.(types.TextContent); ok && tc.Text == "injected" {
							found = true
						}
					}
				}
				if !found {
					t.Errorf("call %d lost the message PrepareCall injected", i)
				}
				if o.Temperature == nil || *o.Temperature != temp {
					t.Errorf("call %d temperature = %v, want 0.25", i, o.Temperature)
				}
			}
		})
	}
}

// recordingTwoToolModel records the options of every model call.
type recordingTwoToolModel struct {
	twoToolCallModel
	rmu  sync.Mutex
	opts []*provider.GenerateOptions
}

func (m *recordingTwoToolModel) record(o *provider.GenerateOptions) {
	m.rmu.Lock()
	m.opts = append(m.opts, o)
	m.rmu.Unlock()
}

func (m *recordingTwoToolModel) seen() []*provider.GenerateOptions {
	m.rmu.Lock()
	defer m.rmu.Unlock()
	return append([]*provider.GenerateOptions(nil), m.opts...)
}

func (m *recordingTwoToolModel) DoGenerate(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.record(o)
	return m.twoToolCallModel.DoGenerate(ctx, o)
}

func (m *recordingTwoToolModel) DoStream(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
	m.record(o)
	return m.twoToolCallModel.DoStream(ctx, o)
}
