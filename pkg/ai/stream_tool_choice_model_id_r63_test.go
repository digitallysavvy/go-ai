package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS stream-language-model-call builds the ToolChoiceViolationError with the
// called model's id (resolvedModel.modelId), not the response's model id, as
// generateText does with stepModel.modelId.
func TestR63StreamToolChoiceViolationCarriesTheCalledModelID(t *testing.T) {
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:     "a",
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		Tools:      []types.Tool{{Name: "t"}},
		Model: &testutil.MockLanguageModel{ModelName: "called-model", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ModelID: "response-model"}},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "No tool call."},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.ConsumeStream()
	var violation *ToolChoiceViolationError
	if !errors.As(res.Err(), &violation) {
		t.Fatalf("Err() = %v, want a ToolChoiceViolationError", res.Err())
	}
	if violation.ModelID != "called-model" {
		t.Fatalf("ModelID = %q, want the called model's id", violation.ModelID)
	}
}
