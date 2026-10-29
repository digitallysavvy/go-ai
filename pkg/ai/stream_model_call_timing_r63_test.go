package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText: the step's performance spreads the model call's
// performance (responseTimeMs from just before doStream to the model call's
// finish) next to stepTimeMs, which also covers tool execution. Go measured
// the response time to the end of the step (tool execution included) and
// from after doStream had returned (connection time excluded).
func TestR63StreamStepPerformanceIsTheModelCallPerformance(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		time.Sleep(60 * time.Millisecond)
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "slow", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "done"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	var mu sync.Mutex
	var callEnds []int64
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:   "a",
		Model:    model,
		StopWhen: []StopCondition{StepCountIs(3)},
		Tools: []types.Tool{{Name: "slow", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			time.Sleep(200 * time.Millisecond)
			return "ok", nil
		}}},
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			mu.Lock()
			callEnds = append(callEnds, e.Performance.ResponseTimeMs)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	steps := res.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	mu.Lock()
	defer mu.Unlock()
	for i, step := range steps {
		p := step.Performance
		if p.ResponseTimeMs < 55 {
			t.Fatalf("step %d ResponseTimeMs = %d, want the model call's time including doStream (>= 60ms)", i, p.ResponseTimeMs)
		}
		if p.ResponseTimeMs != callEnds[i] {
			t.Fatalf("step %d ResponseTimeMs = %d, model call end ResponseTimeMs = %d; want the same", i, p.ResponseTimeMs, callEnds[i])
		}
	}
	if p := steps[0].Performance; p.ResponseTimeMs >= 200 || p.StepTimeMs < 250 {
		t.Fatalf("step 0 ResponseTimeMs = %d, StepTimeMs = %d; want the response time without the 200ms tool and the step time with it", p.ResponseTimeMs, p.StepTimeMs)
	}
}

// TS streamText's stream retry calls the model again through
// streamLanguageModelCall: onLanguageModelCallStart fires for the retry too,
// onStepStart does not.
func TestR63StreamRetryFiresLanguageModelCallStartAgain(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeError, Text: "overloaded"},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "ok"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	retries := 1
	var mu sync.Mutex
	var callStarts, stepStarts int
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:                   "a",
		Model:                    model,
		StreamRetries:            &retries,
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) { mu.Lock(); callStarts++; mu.Unlock() },
		OnStepStart:              func(context.Context, OnStepStartEvent) { mu.Lock(); stepStarts++; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if res.Text() != "ok" || calls != 2 {
		t.Fatalf("text = %q, model calls = %d", res.Text(), calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if callStarts != 2 || stepStarts != 1 {
		t.Fatalf("OnLanguageModelCallStart = %d, OnStepStart = %d; want 2 and 1", callStarts, stepStarts)
	}
}
