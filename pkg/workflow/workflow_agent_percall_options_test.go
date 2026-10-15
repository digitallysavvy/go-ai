package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS WorkflowAgent accepts toolChoice, activeTools and prepareStep on every
// call (generate and stream), overriding the constructor values.
func TestWorkflowPerCallToolChoiceActiveToolsPrepareStep(t *testing.T) {
	mkTools := func() []types.Tool {
		return []types.Tool{{Name: "keep", Description: "k"}, {Name: "drop", Description: "d"}}
	}
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			model := &wfJSONModel{}
			w, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: mkTools(), ToolChoice: types.AutoToolChoice()})
			if err != nil {
				t.Fatal(err)
			}
			stepHookCalls := 0
			hook := func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				stepHookCalls++
				return o, nil
			}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{
					Prompt: "hi", ToolChoice: types.NoneToolChoice(), ActiveTools: []string{"keep"}, PrepareStep: hook,
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := res.ConsumeStream(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{
				Prompt: "hi", ToolChoice: types.NoneToolChoice(), ActiveTools: []string{"keep"}, PrepareStep: hook,
			}); err != nil {
				t.Fatal(err)
			}
			if stepHookCalls == 0 {
				t.Fatal("per-call PrepareStep was not invoked")
			}
			all := append(append([]*provider.GenerateOptions{}, model.opts...), model.wfMockModel.opts...)
			var got *types.ToolChoice
			var tools []string
			for _, o := range all {
				tc := o.ToolChoice
				got = &tc
				tools = nil
				for _, tl := range o.Tools {
					tools = append(tools, tl.Name)
				}
			}
			if got == nil || got.Type != types.NoneToolChoice().Type {
				t.Fatalf("tool choice = %+v, want required", got)
			}
			if len(tools) != 1 || tools[0] != "keep" {
				t.Fatalf("tools = %v, want [keep]", tools)
			}
		})
	}
}

func TestOrderedToolsFromToolSetIsDeterministic(t *testing.T) {
	set := map[string]types.Tool{"c": {}, "a": {}, "b": {}, "d": {}, "e": {}}
	for i := 0; i < 20; i++ {
		got := orderedTools(nil, set)
		for j, want := range []string{"a", "b", "c", "d", "e"} {
			if got[j].Name != want {
				t.Fatalf("iteration %d: order %v", i, got)
			}
		}
	}
}

// A PrepareStep/PrepareCall hook error fails the run instead of being ignored.
func TestWorkflowPrepareHookErrorFailsTheRun(t *testing.T) {
	boom := errors.New("prepare boom")
	hook := func(context.Context, LanguageModelCallOptions) (LanguageModelCallOptions, error) {
		return LanguageModelCallOptions{}, boom
	}
	for name, cfg := range map[string]WorkflowAgent{
		"prepareStep": {PrepareStep: hook},
		"prepareCall": {PrepareCall: hook},
	} {
		t.Run(name, func(t *testing.T) {
			model := &wfJSONModel{}
			cfg.Model = model
			w, err := NewWorkflowAgent(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); !errors.Is(err, boom) {
				t.Fatalf("generate err = %v, want hook error", err)
			}
			res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
			if err == nil {
				_ = res.ConsumeStream()
				err = res.Err()
			}
			if !errors.Is(err, boom) {
				t.Fatalf("stream err = %v, want hook error", err)
			}
			if len(model.opts) != 0 || len(model.wfMockModel.opts) != 0 {
				t.Fatal("the real model was called despite the hook error")
			}
		})
	}
}
