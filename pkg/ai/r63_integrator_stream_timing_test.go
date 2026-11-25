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

// TS streamText: stepStartTimestampMs is taken after prepareStep and the
// step settings, and callStartTimestampMs inside streamLanguageModelCall
// after onLanguageModelCallStart, just before doStream. A slow
// OnLanguageModelCallStart callback is therefore in StepTimeMs but not in
// ResponseTimeMs, in every step, and each step's clocks start afresh.
func TestR63IntegratorStreamStepAndCallClocksStartWhereTSStartsThem(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
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
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:   "a",
		Model:    model,
		StopWhen: []StopCondition{StepCountIs(3)},
		Tools: []types.Tool{{Name: "slow", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			time.Sleep(150 * time.Millisecond)
			return "ok", nil
		}}},
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) {
			time.Sleep(80 * time.Millisecond)
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
	for i, step := range steps {
		p := step.Performance
		if p.ResponseTimeMs >= 60 {
			t.Fatalf("step %d ResponseTimeMs = %d, want the time from just before doStream (< 60ms, without the 80ms OnLanguageModelCallStart)", i, p.ResponseTimeMs)
		}
		if p.StepTimeMs < 80 {
			t.Fatalf("step %d StepTimeMs = %d, want it to include the 80ms OnLanguageModelCallStart", i, p.StepTimeMs)
		}
	}
	if p := steps[0].Performance; p.StepTimeMs < 230 {
		t.Fatalf("step 0 StepTimeMs = %d, want the callback and the 150ms tool (>= 230ms)", p.StepTimeMs)
	}
	if p := steps[1].Performance; p.StepTimeMs >= 200 {
		t.Fatalf("step 1 StepTimeMs = %d, want it to start after step 0 (< 200ms)", p.StepTimeMs)
	}
}

// A stream retry is a new model call (TS streamLanguageModelCall again): its
// response time runs from the retry's doStream, not the first attempt's.
func TestR63IntegratorStreamRetryRestartsTheCallClock(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		if calls == 1 {
			return &r63DelayedStream{
				chunks: []provider.StreamChunk{{Type: provider.ChunkTypeError, Text: "overloaded"}},
				delays: []time.Duration{120 * time.Millisecond},
			}, nil
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
	var ends []int64
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:        "a",
		Model:         model,
		StreamRetries: &retries,
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
	if len(ends) == 0 || ends[len(ends)-1] >= 100 {
		t.Fatalf("model call end ResponseTimeMs = %v, want the retry's own time (< 100ms)", ends)
	}
	if p := res.Steps()[0].Performance; p.ResponseTimeMs >= 100 {
		t.Fatalf("step ResponseTimeMs = %d, want the retry's own time (< 100ms)", p.ResponseTimeMs)
	}
}

// Tool parts held back for stream retries are timed when they arrive and not
// again when they are replayed, so replaying them adds no chunk gaps.
func TestR63IntegratorReplayedToolPartsAddNoChunkGaps(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &r63DelayedStream{
			chunks: []provider.StreamChunk{
				{Type: provider.ChunkTypeToolInputStart, ID: "c1", ToolCall: &types.ToolCall{ID: "c1", ToolName: "t"}},
				{Type: provider.ChunkTypeToolInputDelta, ID: "c1", Text: "{}"},
				{Type: provider.ChunkTypeToolInputEnd, ID: "c1"},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			},
			delays: []time.Duration{0, 0, 0, 0, 120 * time.Millisecond},
		}, nil
	}}
	retries := 1
	var mu sync.Mutex
	var gaps *types.OutputChunkTimingStats
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:        "a",
		Model:         model,
		Tools:         []types.Tool{{Name: "t"}},
		StreamRetries: &retries,
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			mu.Lock()
			gaps = e.Performance.TimeBetweenOutputChunksMs
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.ConsumeStream()
	mu.Lock()
	defer mu.Unlock()
	if gaps != nil && gaps.Max >= 100 {
		t.Fatalf("TimeBetweenOutputChunksMs.Max = %d, want only the arrival gaps (< 100ms)", gaps.Max)
	}
}

// Every StreamText step's model call gets an unset tool choice as auto (TS
// prepareToolChoice per step), not only the first.
func TestR63IntegratorStreamLaterStepGetsAutoToolChoice(t *testing.T) {
	var mu sync.Mutex
	var got []types.ToolChoice
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		calls++
		n := calls
		got = append(got, o.ToolChoice)
		mu.Unlock()
		if n == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "ok"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:   "a",
		Model:    model,
		StopWhen: []StopCondition{StepCountIs(3)},
		Tools: []types.Tool{{Name: "a", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != types.AutoToolChoice() || got[1] != types.AutoToolChoice() {
		t.Fatalf("model tool choices = %+v, want auto for both steps", got)
	}
}
