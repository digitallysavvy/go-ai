package ai

import (
	"context"
	"io"
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

type r63DelayedStream struct {
	chunks []provider.StreamChunk
	delays []time.Duration
	i      int
}

func (s *r63DelayedStream) Next() (*provider.StreamChunk, error) {
	if s.i >= len(s.chunks) {
		return nil, io.EOF
	}
	time.Sleep(s.delays[s.i])
	c := s.chunks[s.i]
	s.i++
	return &c, nil
}
func (s *r63DelayedStream) Err() error   { return nil }
func (s *r63DelayedStream) Close() error { return nil }

// TS stream-language-model-call times output chunks as the model streams
// them, upstream of the tool-part buffering that stream retries add and of
// the tool-part withholding of tool choice none.
func TestR63StreamOutputTimingIsTakenAsChunksArrive(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts func(*StreamTextOptions)
	}{
		{"stream retries buffer tool parts", func(o *StreamTextOptions) { n := 1; o.StreamRetries = &n }},
		{"tool choice none withholds tool parts", func(o *StreamTextOptions) { o.ToolChoice = types.ToolChoice{Type: types.ToolChoiceNone} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			var mu sync.Mutex
			var ttfo *int64
			opts := StreamTextOptions{
				Prompt: "a",
				Model:  model,
				Tools:  []types.Tool{{Name: "t"}},
				OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
					mu.Lock()
					ttfo = e.Performance.TimeToFirstOutputMs
					mu.Unlock()
				},
			}
			tc.opts(&opts)
			res, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			_ = res.ConsumeStream()
			mu.Lock()
			defer mu.Unlock()
			if ttfo == nil || *ttfo >= 100 {
				got := "nil"
				if ttfo != nil {
					got = time.Duration(*ttfo * int64(time.Millisecond)).String()
				}
				t.Fatalf("TimeToFirstOutputMs = %s, want the tool input delta's arrival (< 100ms)", got)
			}
		})
	}
}

// TS streamText: a tool call withheld for tool choice none never reaches the
// step's stream, so a model stream that ends with only that call and no
// finish part produced no output (NoOutputGeneratedError).
func TestR63WithheldToolCallIsNotStepOutput(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt: "a", Model: model, Tools: []types.Tool{{Name: "t"}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.ConsumeStream()
	if !IsNoOutputGeneratedError(res.Err()) {
		t.Fatalf("Err() = %v, want a NoOutputGeneratedError", res.Err())
	}
}
