package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// abortTools returns two tools; running "first" cancels the context so the
// queued "second" call must not start.
func abortTools(cancel context.CancelFunc, ran *[]string) []types.Tool {
	mk := func(name string, doCancel bool) types.Tool {
		return types.Tool{Name: name, Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				*ran = append(*ran, name)
				if doCancel {
					cancel()
				}
				return name, nil
			}}
	}
	return []types.Tool{mk("first", true), mk("second", false)}
}

func TestStreamText_AbortDuringToolsReportsAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var ran []string
	aborted := false
	m := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "first", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c2", ToolName: "second", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}}
	res, err := StreamText(ctx, StreamTextOptions{Model: m, Prompt: "hi", Tools: abortTools(cancel, &ran), ToolCallConcurrency: intp(1), StopWhen: []StopCondition{IsStepCount(3)}, OnAbortEvent: func(context.Context, GenerateTextAbortEvent) { aborted = true }})
	if err != nil {
		t.Fatal(err)
	}
	_, rerr := res.ReadAll()
	if !errors.Is(rerr, context.Canceled) {
		t.Fatalf("want canceled, got %v", rerr)
	}
	if !aborted {
		t.Fatal("OnAbort not called")
	}
	if len(ran) != 1 {
		t.Fatalf("queued tool started after abort: %v", ran)
	}
}

func TestGenerateText_AbortDuringToolsReportsAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var ran []string
	_, err := GenerateText(ctx, GenerateTextOptions{Model: concurrencyGenerateModel("first", "second"), Prompt: "hi", Tools: abortTools(cancel, &ran), ToolCallConcurrency: intp(1), StopWhen: []StopCondition{IsStepCount(3)}})
	t.Logf("ran=%v err=%v", ran, err)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
}
