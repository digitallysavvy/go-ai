package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText (stream-language-model-call): a tool choice violation is an
// `error` part after model-call-end, so the full stream carries it before
// finish-step, onError sees it once, and the run still ends with `finish`
// (finish reason error) and onEnd. Go ended the run without the error part or
// finish.
func TestR59StreamTextToolChoiceViolationIsAnErrorPart(t *testing.T) {
	var mu sync.Mutex
	var onErrors, onEnds int
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:     "a",
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		OnError:    func(context.Context, error) { mu.Lock(); onErrors++; mu.Unlock() },
		OnEndEvent: func(context.Context, OnFinishEvent) { mu.Lock(); onEnds++; mu.Unlock() },
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
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
	parts, chunks := r58FullStreamParts(t, res)
	if want := "start start-step text-start text text-end error finish-step finish"; parts != want {
		t.Fatalf("full stream = %q, want %q", parts, want)
	}
	if !IsToolChoiceViolationError(chunks[5].Err) {
		t.Fatalf("error part = %v, want a ToolChoiceViolationError", chunks[5].Err)
	}
	if fr := res.FinishReason(); fr != types.FinishReasonError {
		t.Fatalf("finish reason = %q, want error", fr)
	}
	if !IsToolChoiceViolationError(res.Err()) {
		t.Fatalf("Err() = %v, want the violation", res.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	if onErrors != 1 || onEnds != 1 {
		t.Fatalf("OnError calls = %d, OnEnd calls = %d; want 1 and 1", onErrors, onEnds)
	}
}
