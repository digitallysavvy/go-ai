package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS toUIMessageStream calls messageMetadata with the stream's own 'finish'
// part, which carries finishReason, rawFinishReason and totalUsage, so a
// messageMetadata callback can put the call's total usage on the message.
func TestR63UIFinishMetadataSeesCallFinishPart(t *testing.T) {
	in, out := int64(3), int64(10)
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "hi"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "end_turn", Usage: &types.Usage{InputTokens: &in, OutputTokens: &out}},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{Prompt: "a", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	var finishPart provider.StreamChunk
	chunks, errs := CreateUIMessageStream(context.Background(), res, UIMessageStreamResultOptions{
		MessageMetadata: func(input map[string]interface{}) map[string]interface{} {
			if input["type"] == "finish" {
				finishPart, _ = input["part"].(provider.StreamChunk)
			}
			return nil
		},
	})
	for range chunks {
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if finishPart.RawFinishReason != "end_turn" || finishPart.Usage == nil || finishPart.Usage.OutputTokens == nil || *finishPart.Usage.OutputTokens != 10 {
		t.Fatalf("finish metadata part = %+v, want the call finish part with raw finish reason and total usage", finishPart)
	}
}
