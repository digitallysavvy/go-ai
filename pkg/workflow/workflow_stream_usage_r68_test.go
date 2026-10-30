package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r68EndUsageCapture struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	ends []telemetry.TelemetryUsage
}

func (c *r68EndUsageCapture) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.OperationType == "ai.streamText" {
		c.ends = append(c.ends, e.Usage)
	}
}

func r68Int(v int64) *int64 { return &v }

func r68WantCounts(t *testing.T, name string, in, out, total *int64, wantIn, wantOut int64) {
	t.Helper()
	if in == nil || out == nil || total == nil {
		t.Fatalf("%s: counts = %v/%v/%v, want all set (TS aggregateUsage)", name, in, out, total)
	}
	if *in != wantIn || *out != wantOut || *total != wantIn+wantOut {
		t.Fatalf("%s: counts = %d/%d/%d, want %d/%d/%d", name, *in, *out, *total, wantIn, wantOut, wantIn+wantOut)
	}
}

// r68UsageModel: step 1 calls a tool and reports input 10 (cache read 4)
// with a provider total of 99 and no output count; step 2 reports output 5
// only, or fails to open when failSecond is set.
func r68UsageModel(failSecond bool) *testutil.MockLanguageModel {
	var mu sync.Mutex
	n := 0
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			tc := types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &tc},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{
					InputTokens: r68Int(10), TotalTokens: r68Int(99), InputDetails: &types.InputTokenDetails{CacheReadTokens: r68Int(4)},
				}},
			}), nil
		}
		if failSecond {
			return nil, errors.New("boom")
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{OutputTokens: r68Int(5)}},
		}), nil
	}}
}

// TS WorkflowAgent stream mode: onEnd usage/totalUsage, telemetry onEnd and
// the stream result's totalUsage are aggregateExecutionUsage(steps, 'stream')
// = aggregateUsage: input/output sums with ?? 0, totalTokens = input + output.
func TestR68WorkflowStreamEndUsageIsAggregateUsage(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		name := "completed"
		wantOut := int64(5)
		if failSecond {
			name, wantOut = "failed", 0
		}
		t.Run(name, func(t *testing.T) {
			capture := &r68EndUsageCapture{}
			var mu sync.Mutex
			var ends []ai.OnFinishEvent
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: r68UsageModel(failSecond),
				Tools: []types.Tool{{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}}},
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{capture}},
				OnEnd:     func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
			})
			if err != nil {
				t.Fatal(err)
			}
			res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = res.ReadAll()
			mu.Lock()
			defer mu.Unlock()
			if len(ends) != 1 {
				t.Fatalf("onEnd calls = %d, want 1", len(ends))
			}
			e := ends[0]
			r68WantCounts(t, "onEnd totalUsage", e.TotalUsage.InputTokens, e.TotalUsage.OutputTokens, e.TotalUsage.TotalTokens, 10, wantOut)
			r68WantCounts(t, "onEnd usage", e.Usage.InputTokens, e.Usage.OutputTokens, e.Usage.TotalTokens, 10, wantOut)
			if d := e.TotalUsage.InputDetails; d == nil || d.CacheReadTokens == nil || *d.CacheReadTokens != 4 {
				t.Fatalf("Go keeps the summed token details; got %+v", d)
			}
			if failSecond {
				return
			}
			u := res.Usage()
			r68WantCounts(t, "result usage", u.InputTokens, u.OutputTokens, u.TotalTokens, 10, 5)
			capture.mu.Lock()
			defer capture.mu.Unlock()
			if len(capture.ends) != 1 {
				t.Fatalf("telemetry ends = %d, want 1", len(capture.ends))
			}
			te := capture.ends[0]
			r68WantCounts(t, "telemetry onEnd", te.InputTokens, te.OutputTokens, te.TotalTokens, 10, 5)
		})
	}
}

// A run that fails before any step: TS onEnd reports aggregateUsage([]) =
// zero counts, not missing ones.
func TestR68WorkflowStreamFailedBeforeStepsReportsZeroUsage(t *testing.T) {
	var mu sync.Mutex
	var ends []ai.OnFinishEvent
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("boom")
		}},
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err == nil {
		_, _ = res.ReadAll()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("onEnd calls = %d, want 1", len(ends))
	}
	u := ends[0].TotalUsage
	r68WantCounts(t, "totalUsage", u.InputTokens, u.OutputTokens, u.TotalTokens, 0, 0)
}

// The total comes from the steps (TS aggregateUsage(steps)), not from a
// transformed call-level finish part.
func TestR68WorkflowStreamEndUsageIgnoresTransformedFinish(t *testing.T) {
	var mu sync.Mutex
	var ends []ai.OnFinishEvent
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: r68Int(10), OutputTokens: r68Int(5), TotalTokens: r68Int(15)}},
			}), nil
		}},
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	transform := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeFinish {
			c.Usage = &types.Usage{InputTokens: r68Int(1), OutputTokens: r68Int(1), TotalTokens: r68Int(2)}
		}
		return []provider.StreamChunk{c}
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", ExperimentalTransform: []ai.StreamTransformFunc{transform}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("onEnd calls = %d, want 1", len(ends))
	}
	u := ends[0].TotalUsage
	r68WantCounts(t, "totalUsage", u.InputTokens, u.OutputTokens, u.TotalTokens, 10, 5)
}

// r68RootCapture records the root's telemetry (every event that is not the
// delegated ai.streamText call's).
type r68RootCapture struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	events []string
	end    *telemetry.TelemetryFinishEvent
}

func (c *r68RootCapture) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.OperationType == "ai.streamText" {
		c.events = append(c.events, "inner-end")
		return
	}
	c.events = append(c.events, "root-end")
	c.end = &e
}

func (c *r68RootCapture) OnError(_ context.Context, _ telemetry.TelemetryErrorEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "error")
}

// TS WorkflowAgent stream mode fires the root's telemetry onEnd when the
// loop ends (notifyEnd), with the last step's data and aggregateUsage, and
// for a failed run onError first; with no steps there is no onEnd.
func TestR68WorkflowStreamRootTelemetryEndsWithRun(t *testing.T) {
	cases := []struct {
		name       string
		model      *testutil.MockLanguageModel
		wantEvents string
		wantOut    int64
	}{
		{"completed", r68UsageModel(false), "inner-end,root-end", 5},
		{"failed", r68UsageModel(true), "error,error,root-end", 0},
		{"failed without steps", &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("boom")
		}}, "error,error", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			capture := &r68RootCapture{}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: c.model,
				Tools: []types.Tool{{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}}},
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{capture}},
			})
			if err != nil {
				t.Fatal(err)
			}
			res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			capture.mu.Lock()
			early := len(capture.events)
			capture.mu.Unlock()
			_, _ = res.ReadAll()
			capture.mu.Lock()
			defer capture.mu.Unlock()
			if c.name == "completed" && early > 0 && capture.events[0] == "root-end" {
				t.Fatal("root telemetry ended when the stream opened, want at the run's end")
			}
			var got string
			for i, e := range capture.events {
				if i > 0 {
					got += ","
				}
				got += e
			}
			if got != c.wantEvents {
				t.Fatalf("events = %s, want %s", got, c.wantEvents)
			}
			if capture.end == nil {
				return
			}
			u := capture.end.Usage
			r68WantCounts(t, "root usage", u.InputTokens, u.OutputTokens, u.TotalTokens, 10, c.wantOut)
			if c.name == "completed" && (capture.end.Text != "done" || capture.end.FinishReason != string(types.FinishReasonStop)) {
				t.Fatalf("root end text/finish = %q/%q, want done/stop", capture.end.Text, capture.end.FinishReason)
			}
		})
	}
}

type r68BlockingStream struct {
	ctx  context.Context
	sent bool
}

func (s *r68BlockingStream) Next() (*provider.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: "partial"}, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}
func (s *r68BlockingStream) Err() error   { return nil }
func (s *r68BlockingStream) Close() error { return nil }

type r68AbortCapture struct {
	r68RootCapture
	aborts int
}

func (c *r68AbortCapture) OnAbort(_ context.Context, _ telemetry.TelemetryAbortEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aborts++
}

// A run cancelled mid-stream ends the root with onAbort (no onEnd: TS
// notifyEnd skips aborted runs).
func TestR68WorkflowStreamRootTelemetryAbortMidStream(t *testing.T) {
	capture := &r68AbortCapture{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return &r68BlockingStream{ctx: ctx}, nil
		}},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{capture}},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi", OnChunk: func(c provider.StreamChunk) {
		if c.Type == provider.ChunkTypeText {
			cancel()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = res.ReadAll()
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.aborts < 2 { // the delegated ai.streamText call and the root
		t.Fatalf("aborts = %d, want the root to abort too (events %v)", capture.aborts, capture.events)
	}
	for _, e := range capture.events {
		if e == "root-end" {
			t.Fatalf("events = %v, want no root onEnd for an aborted run", capture.events)
		}
	}
}
