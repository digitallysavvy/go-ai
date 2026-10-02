package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestToUIMessageStream_DedupesStartStepAgainstStreamStart covers gap A1-1's
// downstream-consumer requirement: once stream.go's own StreamText loop
// emits a genuine ChunkTypeStartStep chunk per step, a provider's own
// ChunkTypeStreamStart chunk (carrying pre-stream warnings) must not also
// produce its own "start-step" UI part for the same step — both map to
// "start-step", and only the first should reach the UI stream.
func TestToUIMessageStream_DedupesStartStepAgainstStreamStart(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{{Message: "w1"}}},
				{Type: provider.ChunkTypeText, ID: "1", Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	chunksCh, errCh := result.ToUIMessageStream(context.Background())
	var startCount, startStepCount int
	for c := range chunksCh {
		switch c["type"] {
		case "start":
			startCount++
		case "start-step":
			startStepCount++
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("err = %v", err)
	}
	if startCount != 1 {
		t.Errorf("start UI parts = %d, want 1", startCount)
	}
	if startStepCount != 1 {
		t.Errorf("start-step UI parts = %d, want 1 (deduped against ChunkTypeStreamStart)", startStepCount)
	}
}

// TestToUIMessageStream_MultiStepFinishStepAndFinishCounts locks down TS
// parity for how stream.go's own once-only call-level ChunkTypeFinish relates
// to the UI stream: TS's to-ui-message-chunk.ts maps 'finish-step' and
// 'finish' to two completely independent UI part types (a 'finish'
// TextStreamPart never produces a "finish-step" UI part), so ChunkTypeFinish
// must NOT also surface as an extra "finish-step" UI part alongside the last
// step's genuine ChunkTypeFinishStep. Exactly one "finish-step" per real step
// (2 steps here), and exactly one "finish" at the very end.
func TestToUIMessageStream_MultiStepFinishStepAndFinishCounts(t *testing.T) {
	t.Parallel()

	step := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			step++
			if step == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, ID: "1", Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	tool := types.Tool{
		Name: "t1",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsLoopFinished()}})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	chunksCh, errCh := result.ToUIMessageStream(context.Background())
	var order []string
	var finishStepCount, finishCount int
	for c := range chunksCh {
		typ, _ := c["type"].(string)
		order = append(order, typ)
		switch typ {
		case "finish-step":
			finishStepCount++
		case "finish":
			finishCount++
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("err = %v", err)
	}
	// One finish-step per step (2 steps); the once-only call-level
	// ChunkTypeFinish that follows the last step's own ChunkTypeFinishStep
	// maps only to "finish", not an extra "finish-step".
	if finishStepCount != 2 {
		t.Errorf("finish-step UI parts = %d, want 2; order = %v", finishStepCount, order)
	}
	if finishCount != 1 {
		t.Errorf("finish UI parts = %d, want 1; order = %v", finishCount, order)
	}
	if order[len(order)-1] != "finish" {
		t.Errorf("last UI part = %q, want \"finish\"; order = %v", order[len(order)-1], order)
	}
}
