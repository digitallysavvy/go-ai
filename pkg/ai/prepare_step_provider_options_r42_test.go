package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// prepareStepProviderOptionsCase returns call options and a PrepareStep that
// returns only override keys, as TS callers do. TS generateText/streamText
// deep-merge them onto the call's providerOptions (mergeObjects).
func prepareStepProviderOptionsCase() (map[string]interface{}, func(context.Context, PrepareStepOptions) PrepareStepOptions) {
	callOptions := map[string]interface{}{
		"openai": map[string]interface{}{"store": false, "user": "u1"},
		"other":  map[string]interface{}{"keep": true},
	}
	prepare := func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
		return PrepareStepOptions{ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"user": "step"},
		}}
	}
	return callOptions, prepare
}

func wantMergedProviderOptions() map[string]interface{} {
	return map[string]interface{}{
		"openai": map[string]interface{}{"store": false, "user": "step"},
		"other":  map[string]interface{}{"keep": true},
	}
}

func TestGenerateText_PrepareStepProviderOptionsDeepMerge(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var seen []map[string]interface{}
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			seen = append(seen, opts.ProviderOptions)
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "noop", Arguments: map[string]interface{}{}}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	callOptions, prepare := prepareStepProviderOptionsCase()
	maxSteps := 2
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:           model,
		Prompt:          "hi",
		ProviderOptions: callOptions,
		MaxSteps:        &maxSteps,
		Tools: []types.Tool{{Name: "noop", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		PrepareStep: prepare,
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("model calls = %d, want 2", len(seen))
	}
	for i, got := range seen {
		if !reflect.DeepEqual(got, wantMergedProviderOptions()) {
			t.Fatalf("step %d providerOptions = %#v, want %#v", i, got, wantMergedProviderOptions())
		}
	}
}

func TestStreamText_PrepareStepProviderOptionsDeepMerge(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var seen []map[string]interface{}
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			seen = append(seen, opts.ProviderOptions)
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "noop", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	callOptions, prepare := prepareStepProviderOptionsCase()
	maxSteps := 2
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:           model,
		Prompt:          "hi",
		ProviderOptions: callOptions,
		MaxSteps:        &maxSteps,
		Tools: []types.Tool{{Name: "noop", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		PrepareStep: prepare,
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("model calls = %d, want 2", len(seen))
	}
	for i, got := range seen {
		if !reflect.DeepEqual(got, wantMergedProviderOptions()) {
			t.Fatalf("step %d providerOptions = %#v, want %#v", i, got, wantMergedProviderOptions())
		}
	}
}
