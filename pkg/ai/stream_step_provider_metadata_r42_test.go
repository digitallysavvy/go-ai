package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText takes a step's providerMetadata from its model-call-end
// (finish) part only and resets it every step; metadata on text or
// reasoning parts stays on those parts.
func TestStreamText_StepProviderMetadataComesFromFinishChunk(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "noop", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, ProviderMetadata: json.RawMessage(`{"p":{"step":1}}`)},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoning, Text: "", ProviderMetadata: json.RawMessage(`{"p":{"signature":"sig"}}`)},
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	maxSteps := 2
	var finishStepMeta []json.RawMessage
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "hi",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{{Name: "noop", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeFinishStep {
			finishStepMeta = append(finishStepMeta, chunk.ProviderMetadata)
		}
	}
	steps := result.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	if want := map[string]interface{}{"p": map[string]interface{}{"step": float64(1)}}; !reflect.DeepEqual(steps[0].ProviderMetadata, want) {
		t.Fatalf("step 0 providerMetadata = %v, want %v", steps[0].ProviderMetadata, want)
	}
	if steps[1].ProviderMetadata != nil {
		t.Fatalf("step 1 providerMetadata = %v, want nil (its finish chunk has none)", steps[1].ProviderMetadata)
	}
	if len(result.ProviderMetadata()) != 0 {
		t.Fatalf("result providerMetadata = %s, want none (final step)", result.ProviderMetadata())
	}
	if len(finishStepMeta) != 2 || string(finishStepMeta[0]) != `{"p":{"step":1}}` || len(finishStepMeta[1]) != 0 {
		t.Fatalf("finish-step providerMetadata = %q", finishStepMeta)
	}
}
