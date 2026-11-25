package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func portcarrywfInt(v int64) *int64 { return &v }

// TS 1876747927 (workflow-agent-contract.test.ts 'aggregates stream usage like
// core, keeping token details'): stream totalUsage, and the usage and
// totalUsage of onEnd, keep the token details.
func TestPortcarrywfStreamTotalUsageKeepsTokenDetails(t *testing.T) {
	var mu sync.Mutex
	n := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			tc := types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &tc},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{
					InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2),
					InputDetails:  &types.InputTokenDetails{NoCacheTokens: portcarrywfInt(2), CacheReadTokens: portcarrywfInt(1)},
					OutputDetails: &types.OutputTokenDetails{TextTokens: portcarrywfInt(2)},
				}},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{
				InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2),
				InputDetails:  &types.InputTokenDetails{NoCacheTokens: portcarrywfInt(2), CacheReadTokens: portcarrywfInt(1)},
				OutputDetails: &types.OutputTokenDetails{TextTokens: portcarrywfInt(2)},
			}},
		}), nil
	}}
	var ends []ai.OnFinishEvent
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	u := res.Usage()
	if u.InputTokens == nil || *u.InputTokens != 6 || u.OutputTokens == nil || *u.OutputTokens != 4 || u.TotalTokens == nil || *u.TotalTokens != 10 {
		t.Fatalf("totalUsage counts = %+v, want 6/4/10", u)
	}
	if d := u.InputDetails; d == nil || *d.NoCacheTokens != 4 || *d.CacheReadTokens != 2 || d.CacheWriteTokens != nil {
		t.Fatalf("totalUsage input details = %+v, want noCache 4, cacheRead 2, cacheWrite unreported", d)
	}
	if d := u.OutputDetails; d == nil || *d.TextTokens != 4 || d.ReasoningTokens != nil {
		t.Fatalf("totalUsage output details = %+v, want text 4, reasoning unreported", d)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("onEnd calls = %d, want 1", len(ends))
	}
	for name, got := range map[string]types.Usage{"usage": ends[0].Usage, "totalUsage": ends[0].TotalUsage} {
		if got.InputDetails == nil || *got.InputDetails.CacheReadTokens != 2 || *got.TotalTokens != 10 {
			t.Fatalf("onEnd %s = %+v, want the stream totalUsage", name, got)
		}
	}
}

// TS 1876747927 ('keeps unknown stream usage counts undefined like core'):
// a count the provider did not report stays nil, not 0.
func TestPortcarrywfStreamTotalUsageKeepsUnknownCountsUnreported(t *testing.T) {
	w, err := NewWorkflowAgent(WorkflowAgent{Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "Done."},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}), nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if u := res.Usage(); u.InputTokens != nil || u.OutputTokens != nil || u.TotalTokens != nil {
		t.Fatalf("totalUsage = %+v, want every count unreported", u)
	}
}

func portcarrywfFailingModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, errors.New("model exploded")
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("model exploded")
		},
	}
}

// TS b161730659 ('calls a constructor onError callback like ToolLoopAgent,
// before the call callback'): the constructor callback runs first, for generate
// and stream.
func TestPortcarrywfConstructorOnErrorRunsBeforeCallOnError(t *testing.T) {
	var calls []string
	record := func(label string) ErrorCallback {
		return func(_ context.Context, err error) { calls = append(calls, label+": "+err.Error()) }
	}
	zero := 0
	w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfFailingModel(), MaxRetries: &zero, OnError: record("constructor")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", OnError: record("generate call")}); err == nil {
		t.Fatal("generate: want the model error")
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", OnError: record("stream call")})
	if err == nil {
		_, err = res.ReadAll()
	}
	if err == nil {
		t.Fatal("stream: want the model error")
	}
	want := []string{
		"constructor: model exploded", "generate call: model exploded",
		"constructor: model exploded", "stream call: model exploded",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

// TS b161730659 ('keeps the model error when onError throws'): a panic in
// onError replaces neither the run's error nor its result.
func TestPortcarrywfOnErrorPanicKeepsModelError(t *testing.T) {
	zero := 0
	boom := func(context.Context, error) { panic("onError exploded") }
	w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfFailingModel(), MaxRetries: &zero, OnError: boom})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", OnError: boom}); err == nil || !strings.Contains(err.Error(), "model exploded") {
		t.Fatalf("generate error = %v, want the model error", err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", OnError: boom})
	if err == nil {
		_, err = res.ReadAll()
	}
	if err == nil || !strings.Contains(err.Error(), "model exploded") {
		t.Fatalf("stream error = %v, want the model error", err)
	}
}

// TS b161730659 ('calls constructor and call onAbort callbacks and ignores
// errors they throw'): an already-aborted stream() calls both, constructor
// first, with no steps; the panic of the first changes nothing.
func TestPortcarrywfOnAbortConstructorThenCallPanicIgnored(t *testing.T) {
	var calls []string
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &testutil.MockLanguageModel{},
		OnAbort: func(_ context.Context, steps []types.StepResult) {
			calls = append(calls, "constructor")
			if len(steps) != 0 {
				t.Errorf("constructor steps = %d, want 0", len(steps))
			}
			panic("onAbort exploded")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi", OnAbort: func(context.Context, []types.StepResult) { calls = append(calls, "call") }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
	if len(calls) != 2 || calls[0] != "constructor" || calls[1] != "call" {
		t.Fatalf("calls = %v, want [constructor call]", calls)
	}
}
