package ai

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR67PrepareStepSystemOverride: a PrepareStep that sets System (the
// documented "effective instructions/system prompt for the next step") and
// returns the options it got overrides the step instructions (TS
// prepareStepResult.system). The input's Instructions pointer, passed back
// unchanged, must not win over it; a changed Instructions still does.
func TestR67PrepareStepSystemOverride(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(PrepareStepOptions) PrepareStepOptions
		want    string
		// wantStep1, when set, is the second step's system.
		wantStep1 string
	}{
		{"system", func(s PrepareStepOptions) PrepareStepOptions { s.System = "step system"; return s }, "step system", ""},
		{"system per step", func(s PrepareStepOptions) PrepareStepOptions {
			s.System = "step " + strconv.Itoa(s.StepNumber)
			return s
		}, "step 0", "step 1"},
		{"instructions", func(s PrepareStepOptions) PrepareStepOptions {
			v := "step instructions"
			s.Instructions = &v
			s.System = "ignored"
			return s
		}, "step instructions", ""},
		{"instructions written through", func(s PrepareStepOptions) PrepareStepOptions { *s.Instructions = "written"; return s }, "written", ""},
		{"unchanged", func(s PrepareStepOptions) PrepareStepOptions { return s }, "base", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var systems []string
			calls := 0
			// The first call of each run asks for a tool, so the run has two
			// steps (StreamText's first-step and next-step paths).
			record := func(opts *provider.GenerateOptions) bool {
				mu.Lock()
				defer mu.Unlock()
				systems = append(systems, opts.Prompt.System)
				calls++
				return calls%2 == 1
			}
			toolCall := types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}
			model := &testutil.MockLanguageModel{
				DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					if record(opts) {
						return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{toolCall}}, nil
					}
					return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
				},
				DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
					if record(opts) {
						return testutil.NewMockTextStream([]provider.StreamChunk{
							{Type: provider.ChunkTypeToolCall, ToolCall: &toolCall},
							{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
						}), nil
					}
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeText, Text: "ok"},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
					}), nil
				},
			}
			tools := []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "r", nil
			}}}
			stop := []StopCondition{IsStepCount(2)}
			prepare := func(_ context.Context, s PrepareStepOptions) PrepareStepOptions { return tc.prepare(s) }
			if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", System: "base", Tools: tools, StopWhen: stop, PrepareStep: prepare}); err != nil {
				t.Fatal(err)
			}
			res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", System: "base", Tools: tools, StopWhen: stop, PrepareStep: prepare})
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Text()
			mu.Lock()
			defer mu.Unlock()
			if len(systems) != 4 {
				t.Fatalf("model calls = %d, want 4", len(systems))
			}
			for i, got := range systems {
				want := tc.want
				if i%2 == 1 && tc.wantStep1 != "" {
					want = tc.wantStep1
				}
				if got != want {
					t.Fatalf("model call %d (generate 0-1, stream 2-3) system = %q, want %q (all: %q)", i, got, want, systems)
				}
			}
		})
	}
}
