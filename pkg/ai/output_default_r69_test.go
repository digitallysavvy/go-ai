package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Owner ruling 2026-10-30 (round 69): with no Output option, GenerateText's
// Output and StreamText's Output() are the final step's text, as TS
// `output ?? text()` (generate-text.ts parses only for a stop finish or a
// non-tool-calls finish with text; stream-text.ts parseOutput(finalStep)
// always). The end event carries no output without a spec.

func TestR69GenerateTextOutputDefaultsToText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		text   string
		reason types.FinishReason
		want   any
	}{
		{"stop", "hello", types.FinishReasonStop, "hello"},
		{"stop with empty text", "", types.FinishReasonStop, ""},
		{"length with text", "partial", types.FinishReasonLength, "partial"},
		{"length without text", "", types.FinishReasonLength, nil},
		{"tool-calls finish", "thinking", types.FinishReasonToolCalls, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: tc.text, FinishReason: tc.reason}, nil
			}}
			var mu sync.Mutex
			var eventOutput any = "unset"
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:  model,
				Prompt: "hi",
				OnEndEvent: func(_ context.Context, e OnFinishEvent) {
					mu.Lock()
					eventOutput = e.Output
					mu.Unlock()
				},
			})
			if err != nil {
				t.Fatalf("GenerateText: %v", err)
			}
			if result.Output != tc.want {
				t.Errorf("Output = %#v, want %#v", result.Output, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if eventOutput != nil {
				t.Errorf("end event Output = %#v, want nil without an Output spec", eventOutput)
			}
		})
	}
}

func TestR69GenerateTextOutputDefaultIsLastStepText(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					Text:         "first ",
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
				}, nil
			}
			return &types.GenerateResult{Text: "final", FinishReason: types.FinishReasonStop}, nil
		},
	}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if result.Output != "final" {
		t.Errorf("Output = %#v, want the last step's text %q", result.Output, "final")
	}
}

func TestR69StreamTextOutputDefaultsToText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		chunks []provider.StreamChunk
		want   any
	}{
		{"stop", []provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "he"},
			{Type: provider.ChunkTypeText, Text: "llo"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}, "hello"},
		{"empty text", []provider.StreamChunk{
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}, ""},
		// TS parseOutput(finalStep) has no finish-reason gate.
		{"length", []provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "cut"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
		}, "cut"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream(tc.chunks), nil
			}}
			var mu sync.Mutex
			var eventOutput any = "unset"
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model:  model,
				Prompt: "hi",
				OnEndEvent: func(_ context.Context, e OnFinishEvent) {
					mu.Lock()
					eventOutput = e.Output
					mu.Unlock()
				},
			})
			if err != nil {
				t.Fatalf("StreamText: %v", err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if got := result.Output(); got != tc.want {
				t.Errorf("Output() = %#v, want %#v", got, tc.want)
			}
			if err := result.OutputErr(); err != nil {
				t.Errorf("OutputErr() = %v, want nil", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if eventOutput != nil {
				t.Errorf("end event Output = %#v, want nil without an Output spec", eventOutput)
			}
		})
	}
}

func TestR69StreamTextOutputDefaultFailedStream(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return nil, errors.New("boom")
	}}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	_, _ = result.ReadAll()
	// TS rejects `.output` when no step was recorded.
	if got := result.Output(); got != nil {
		t.Errorf("Output() = %#v, want nil for a failed run", got)
	}
}

func TestR69StreamTextOutputDefaultIsLastStepText(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "first "},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "final"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := result.Output(); got != "final" {
		t.Errorf("Output() = %#v, want the last step's text %q", got, "final")
	}
}

func TestR69StreamTextOutputSpecUnchanged(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: `{"a":1}`},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Output: JSONOutput(JSONOutputOptions{})})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if _, isString := result.Output().(string); isString {
		t.Errorf("Output() = %#v, want the parsed JSON, not the text", result.Output())
	}
}
