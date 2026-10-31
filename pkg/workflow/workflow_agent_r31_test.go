package workflow

import (
	"context"
	"testing"
)

// TS prepareCall and prepareStep may return `model` to run the call (or the
// step) on another model.
func TestWorkflowPrepareHooksCanOverrideTheModel(t *testing.T) {
	for _, hookName := range []string{"prepareCall", "prepareStep"} {
		for _, stream := range []bool{false, true} {
			name := hookName + "/generate"
			if stream {
				name = hookName + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				base, alt := &wfMockModel{}, &wfMockModel{}
				hook := func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					// Later steps start from the override: TS's iterator keeps
					// currentModel across steps.
					if want := interface{}(base); o.StepNumber > 0 {
						if o.Model != alt {
							t.Errorf("step %d hook saw model %v, want the override", o.StepNumber, o.Model)
						}
					} else if o.Model != want {
						t.Errorf("hook saw model %v, want the agent model", o.Model)
					}
					o.Model = alt
					return o, nil
				}
				cfg := WorkflowAgent{Model: base}
				if hookName == "prepareCall" {
					cfg.PrepareCall = hook
				} else {
					cfg.PrepareStep = hook
				}
				w, err := NewWorkflowAgent(cfg)
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
				if len(alt.opts) == 0 || len(base.opts) != 0 {
					t.Fatalf("alt model calls = %d, base model calls = %d; want the override to run the step", len(alt.opts), len(base.opts))
				}
			})
		}
	}
}
