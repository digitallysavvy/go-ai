package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS toUIMessageChunk forwards providerExecuted whenever it is not undefined,
// so an explicit false (types.ToolCall.ProviderExecutedSet) reaches the UI
// chunk, and an unset one is omitted.
func TestToUIMessageChunk_ForwardsExplicitProviderExecutedFalse(t *testing.T) {
	explicit := &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}, ProviderExecutedSet: true}
	unset := &types.ToolCall{ID: "c2", ToolName: "t", Arguments: map[string]interface{}{}}
	invalid := &types.ToolCall{ID: "c3", ToolName: "t", Invalid: true, ProviderExecutedSet: true}
	for _, tc := range []struct {
		chunk provider.StreamChunk
		want  interface{}
	}{
		{provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: explicit}, false},
		{provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: explicit}, false},
		{provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: invalid}, false},
		{provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: unset}, nil},
		{provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: unset}, nil},
	} {
		chunk, ok := ToUIMessageChunk(tc.chunk, UIMessageStreamResultOptions{})
		if !ok {
			t.Fatalf("%s suppressed", tc.chunk.Type)
		}
		got, present := chunk["providerExecuted"]
		if tc.want == nil {
			if present {
				t.Fatalf("%s %s: providerExecuted = %v, want omitted", tc.chunk.Type, tc.chunk.ToolCall.ID, got)
			}
			continue
		}
		if got != tc.want {
			t.Fatalf("%s %s: providerExecuted = %v (present %v), want %v", tc.chunk.Type, tc.chunk.ToolCall.ID, got, present, tc.want)
		}
	}
}

// The explicit false survives parsing in StreamText and reaches the UI stream
// and ToModelOutput's tool call.
func TestStreamText_ExplicitProviderExecutedFalseReachesUIStream(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "c1", ToolName: "noop", ProviderExecutedSet: true}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "noop", Arguments: map[string]interface{}{}, ProviderExecutedSet: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var seen *types.ToolCall
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{
			Name: "noop",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
			ToModelOutput: func(_ context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				seen = o.ToolCall
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}, nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := result.ToUIMessageStream(context.Background())
	got := map[string]interface{}{}
	for c := range chunks {
		if typ, _ := c["type"].(string); typ == "tool-input-start" || typ == "tool-input-available" {
			v, ok := c["providerExecuted"]
			if !ok {
				v = "omitted"
			}
			got[typ] = v
		}
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if got["tool-input-start"] != false || got["tool-input-available"] != false {
		t.Fatalf("providerExecuted = %#v, want explicit false on both", got)
	}
	if seen == nil || !seen.ProviderExecutedSet || seen.ProviderExecuted {
		t.Fatalf("ToModelOutput tool call = %#v", seen)
	}
}

func TestGenerateText_ToModelOutputToolCallKeepsProviderExecutedSet(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "noop", Arguments: map[string]interface{}{}, ProviderExecutedSet: true}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	var seen *types.ToolCall
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{
			Name: "noop",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
			ToModelOutput: func(_ context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				seen = o.ToolCall
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}, nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == nil || !seen.ProviderExecutedSet {
		t.Fatalf("ToModelOutput tool call = %#v", seen)
	}
}
