package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText retries streamLanguageModelCall as a whole when doStream
// fails to open, so each retried open fires onLanguageModelCallStart again
// and takes a new callStartTimestampMs; onStepStart fires once. Checked for
// the first step and for a later step.
func TestR64StreamOpenRetryFiresCallStartAndRestartsClock(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1, 3:
			time.Sleep(120 * time.Millisecond)
			return nil, &providererrors.ProviderError{Provider: "mock", StatusCode: 500, Message: "temporary", ResponseHeaders: map[string]string{"retry-after-ms": "0"}}
		case 2:
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "ok"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	var starts, stepStarts int
	var ends []int64
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:   "a",
		Model:    model,
		StopWhen: []StopCondition{StepCountIs(3)},
		Tools: []types.Tool{{Name: "a", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		OnStepStart: func(context.Context, OnStepStartEvent) {
			mu.Lock()
			stepStarts++
			mu.Unlock()
		},
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) {
			mu.Lock()
			starts++
			mu.Unlock()
		},
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			mu.Lock()
			ends = append(ends, e.Performance.ResponseTimeMs)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 4 {
		t.Fatalf("DoStream calls = %d, want 4", calls)
	}
	if starts != 4 {
		t.Fatalf("OnLanguageModelCallStart fired %d times, want 4 (once per attempt)", starts)
	}
	if stepStarts != 2 {
		t.Fatalf("OnStepStart fired %d times, want 2 (once per step)", stepStarts)
	}
	if len(ends) != 2 {
		t.Fatalf("OnLanguageModelCallEnd fired %d times, want 2", len(ends))
	}
	for i, ms := range ends {
		if ms >= 100 {
			t.Fatalf("model call %d ResponseTimeMs = %d, want the successful attempt's own time (< 100ms)", i, ms)
		}
	}
	for i, step := range res.Steps() {
		if p := step.Performance; p.ResponseTimeMs >= 100 {
			t.Fatalf("step %d ResponseTimeMs = %d, want the successful attempt's own time (< 100ms)", i, p.ResponseTimeMs)
		}
	}
}
