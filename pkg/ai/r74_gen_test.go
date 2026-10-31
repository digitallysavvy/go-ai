package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r74genIntPtr(v int) *int { return &v }

// TS executeTools fires onToolExecutionStart in call order for the calls that
// execute. A call between them that never reaches its start (here a client
// tool without Execute, and a provider-executed call) must not let a later
// call start before an earlier one: the chain is transitive.
func TestR74Gen_ParallelToolStartOrderSkipsCallsWithoutStart(t *testing.T) {
	exec := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	client := types.Tool{Name: "client", Parameters: map[string]interface{}{"type": "object"}}
	provider := types.Tool{Name: "web", Parameters: map[string]interface{}{"type": "object"}, ProviderExecuted: true}
	for _, concurrency := range []*int{nil, r74genIntPtr(4), r74genIntPtr(2)} {
		var mu sync.Mutex
		var starts []string
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r72genSeqModel(t,
				r72genCalls(
					types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-2", ToolName: "client", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-3", ToolName: "web", Arguments: map[string]interface{}{}, ProviderExecuted: true},
					types.ToolCall{ID: "call-4", ToolName: "tool1", Arguments: map[string]interface{}{}}),
				r72genText("done")),
			Prompt:              "go",
			Tools:               []types.Tool{exec, client, provider},
			ToolCallConcurrency: concurrency,
			OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
				if e.ToolCall.ID == "call-1" {
					time.Sleep(40 * time.Millisecond)
				}
				mu.Lock()
				defer mu.Unlock()
				starts = append(starts, e.ToolCall.ID)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := strings.Join(starts, ",")
		mu.Unlock()
		if got != "call-1,call-4" {
			t.Errorf("concurrency=%v: start order = %s, want call-1,call-4", concurrency, got)
		}
	}
}

func r74genExecTool() types.Tool {
	return types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
}

// StreamText shares executeToolsLimited: same transitive start order.
func TestR74Gen_StreamParallelToolStartOrderSkipsCallsWithoutStart(t *testing.T) {
	n := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			n++
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "client", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-3", ToolName: "tool1", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var mu sync.Mutex
	var starts []string
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "go",
		Tools:  []types.Tool{r74genExecTool(), {Name: "client", Parameters: map[string]interface{}{"type": "object"}}},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			if e.ToolCall.ID == "call-1" {
				time.Sleep(40 * time.Millisecond)
			}
			mu.Lock()
			defer mu.Unlock()
			starts = append(starts, e.ToolCall.ID)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(starts, ","); got != "call-1,call-3" {
		t.Fatalf("start order = %s, want call-1,call-3", got)
	}
}

// A start callback that panics still releases the next call (Notify recovers
// the panic and the gate is closed after it), and both calls execute.
func TestR74Gen_ParallelToolStartGatePanickingCallback(t *testing.T) {
	var mu sync.Mutex
	var starts []string
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(
				types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
				types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{}}),
			r72genText("done")),
		Prompt:   "go",
		Tools:    []types.Tool{r74genExecTool()},
		StopWhen: []StopCondition{StepCountIs(2)},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			mu.Lock()
			starts = append(starts, e.ToolCall.ID)
			mu.Unlock()
			if e.ToolCall.ID == "call-1" {
				panic("boom")
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(starts, ","); got != "call-1,call-2" {
		t.Fatalf("start order = %s, want call-1,call-2", got)
	}
	if len(res.Steps) != 2 || len(res.Steps[0].ToolResults) != 2 {
		t.Fatalf("steps = %d, first step results = %+v", len(res.Steps), res.Steps[0].ToolResults)
	}
}

// A start callback that blocks until the run is cancelled must not leave the
// later call waiting on its gate forever: the gate wait ends with ctx.
func TestR74Gen_ParallelToolStartGateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call2Started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := GenerateText(ctx, GenerateTextOptions{
			Model: r72genSeqModel(t,
				r72genCalls(
					types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{}}),
				r72genText("done")),
			Prompt: "go",
			Tools:  []types.Tool{r74genExecTool()},
			OnToolExecutionStart: func(cbCtx context.Context, e OnToolCallStartEvent) {
				if e.ToolCall.ID == "call-1" {
					<-ctx.Done()
					return
				}
				close(call2Started)
			},
		})
		done <- err
	}()
	select {
	case <-call2Started:
		t.Fatal("call-2 started before call-1 fired its start")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		// The step ends after the tools ran (default StepCountIs(1)); the
		// point is that the run returns at all.
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GenerateText did not return after cancellation")
	}
}
