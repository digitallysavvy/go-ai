package workflow

import (
	"context"
	"errors"
	"strings"
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

// TS workflow-agent.test.ts "should allow stream options to override
// constructor generation settings": per-call Partial<GenerationSettings>
// replace the agent's, for stream and generate; unset ones keep the agent's.
func TestWorkflowPerCallGenerationSettingsOverrideAgent(t *testing.T) {
	temp, topP, seed := 0.7, 0.9, 42
	callTemp, callMax := 0.3, 500
	low, none := types.ReasoningLevel("low"), types.ReasoningLevel("none")
	run := func(t *testing.T, stream bool) *provider.GenerateOptions {
		t.Helper()
		m := &wfMockModel{}
		a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Temperature: &temp, TopP: &topP, Seed: &seed, Reasoning: &low,
			Headers: map[string]string{"x-agent": "1"}})
		if err != nil {
			t.Fatal(err)
		}
		if stream {
			st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "p", Temperature: &callTemp, MaxTokens: &callMax, Reasoning: &none,
				Headers: map[string]string{"x-call": "2"}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"a": 1}}})
			if err != nil {
				t.Fatal(err)
			}
			_ = st.ConsumeStream()
		} else {
			if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "p", Temperature: &callTemp, MaxTokens: &callMax, Reasoning: &none,
				Headers: map[string]string{"x-call": "2"}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"a": 1}}}); err != nil {
				t.Fatal(err)
			}
		}
		return m.opts[0]
	}
	for _, stream := range []bool{true, false} {
		o := run(t, stream)
		if o.Temperature == nil || *o.Temperature != 0.3 || o.MaxTokens == nil || *o.MaxTokens != 500 {
			t.Fatalf("stream=%v temperature/maxTokens = %v/%v", stream, o.Temperature, o.MaxTokens)
		}
		if o.TopP == nil || *o.TopP != 0.9 || o.Seed == nil || *o.Seed != 42 {
			t.Fatalf("stream=%v agent topP/seed lost: %v/%v", stream, o.TopP, o.Seed)
		}
		if o.Reasoning == nil || *o.Reasoning != "none" {
			t.Fatalf("stream=%v reasoning = %v", stream, o.Reasoning)
		}
		if o.Headers["x-call"] != "2" || o.Headers["x-agent"] != "" || !strings.Contains(o.Headers["user-agent"], "ai-sdk-agent/workflow") {
			t.Fatalf("stream=%v headers = %v", stream, o.Headers)
		}
		if o.ProviderOptions["openai"] == nil {
			t.Fatalf("stream=%v providerOptions = %v", stream, o.ProviderOptions)
		}
	}
}
