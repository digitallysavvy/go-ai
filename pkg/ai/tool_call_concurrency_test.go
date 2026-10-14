package ai

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type concurrencyRecorder struct {
	mu      sync.Mutex
	events  []string
	running int
	maxSeen int
}

func (r *concurrencyRecorder) enter(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running++
	if r.running > r.maxSeen {
		r.maxSeen = r.running
	}
	r.events = append(r.events, name+":start")
}

func (r *concurrencyRecorder) leave(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running--
	r.events = append(r.events, name+":end")
}

func (r *concurrencyRecorder) snapshot() ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...), r.maxSeen
}

func concurrencyTools(rec *concurrencyRecorder, names ...string) []types.Tool {
	var tools []types.Tool
	for _, name := range names {
		name := name
		tools = append(tools, types.Tool{
			Name:       name,
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				rec.enter(name)
				time.Sleep(30 * time.Millisecond)
				rec.leave(name)
				return name, nil
			},
		})
	}
	return tools
}

func concurrencyGenerateModel(names ...string) *testutil.MockLanguageModel {
	calls := 0
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			var toolCalls []types.ToolCall
			for i, name := range names {
				toolCalls = append(toolCalls, types.ToolCall{ID: "call-" + string(rune('1'+i)), ToolName: name, Arguments: map[string]interface{}{}})
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: toolCalls}, nil
		},
	}
}

func intp(i int) *int { return &i }

func TestPrepareToolCallConcurrency(t *testing.T) {
	for _, bad := range []int{0, -1} {
		if _, err := prepareToolCallConcurrency(intp(bad)); !providererrors.IsInvalidArgumentError(err) {
			t.Errorf("concurrency %d: err = %v, want InvalidArgumentError", bad, err)
		}
	}
	if n, err := prepareToolCallConcurrency(nil); n != 0 || err != nil {
		t.Errorf("nil: %d, %v", n, err)
	}
	if n, err := prepareToolCallConcurrency(intp(2)); n != 2 || err != nil {
		t.Errorf("2: %d, %v", n, err)
	}
}

func TestGenerateText_ToolCallConcurrency(t *testing.T) {
	t.Run("1 runs sequentially in model order", func(t *testing.T) {
		rec := &concurrencyRecorder{}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: concurrencyGenerateModel("first", "second", "third"), Prompt: "hi",
			Tools: concurrencyTools(rec, "first", "second", "third"), ToolCallConcurrency: intp(1),
			StopWhen: []StopCondition{IsStepCount(2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		events, max := rec.snapshot()
		want := []string{"first:start", "first:end", "second:start", "second:end", "third:start", "third:end"}
		if !reflect.DeepEqual(events, want) || max != 1 {
			t.Fatalf("events = %v (max %d), want %v", events, max, want)
		}
	})

	t.Run("2 bounds concurrency and keeps result order", func(t *testing.T) {
		rec := &concurrencyRecorder{}
		result, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: concurrencyGenerateModel("first", "second", "third"), Prompt: "hi",
			Tools: concurrencyTools(rec, "first", "second", "third"), ToolCallConcurrency: intp(2),
			StopWhen: []StopCondition{IsStepCount(2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, max := rec.snapshot()
		if max != 2 {
			t.Fatalf("max concurrent = %d, want 2", max)
		}
		var order []string
		for _, r := range result.Steps[0].ToolResults {
			order = append(order, r.ToolName)
		}
		if !reflect.DeepEqual(order, []string{"first", "second", "third"}) {
			t.Fatalf("result order = %v", order)
		}
	})

	t.Run("rejects invalid concurrency before calling the model", func(t *testing.T) {
		model := concurrencyGenerateModel("first")
		_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", ToolCallConcurrency: intp(0)})
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != "toolCallConcurrency" {
			t.Fatalf("err = %v", err)
		}
		if len(model.GenerateCalls) != 0 {
			t.Fatal("model must not be called")
		}
	})
}

func TestStreamText_ToolCallConcurrency(t *testing.T) {
	newModel := func(names ...string) *testutil.MockLanguageModel {
		step := 0
		return &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				step++
				if step > 1 {
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeText, Text: "done"},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
					}), nil
				}
				var chunks []provider.StreamChunk
				for i, name := range names {
					chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-" + string(rune('1'+i)), ToolName: name, Arguments: map[string]interface{}{}}})
				}
				chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})
				return testutil.NewMockTextStream(chunks), nil
			},
		}
	}

	t.Run("1 runs sequentially in model order", func(t *testing.T) {
		rec := &concurrencyRecorder{}
		result, err := StreamText(context.Background(), StreamTextOptions{
			Model: newModel("first", "second"), Prompt: "hi", Tools: concurrencyTools(rec, "first", "second"),
			ToolCallConcurrency: intp(1), StopWhen: []StopCondition{IsStepCount(2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		events, max := rec.snapshot()
		want := []string{"first:start", "first:end", "second:start", "second:end"}
		if !reflect.DeepEqual(events, want) || max != 1 {
			t.Fatalf("events = %v (max %d)", events, max)
		}
	})

	t.Run("rejects invalid concurrency", func(t *testing.T) {
		_, err := StreamText(context.Background(), StreamTextOptions{Model: newModel("first"), Prompt: "hi", ToolCallConcurrency: intp(-1)})
		if !providererrors.IsInvalidArgumentError(err) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestExecuteToolsLimited_DoesNotStartQueuedCallsAfterAbort(t *testing.T) {
	rec := &concurrencyRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	tools := []types.Tool{{
		Name:       "t",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			rec.enter("t")
			cancel()
			rec.leave("t")
			return "ok", nil
		},
	}}
	calls := []types.ToolCall{{ID: "1", ToolName: "t"}, {ID: "2", ToolName: "t"}, {ID: "3", ToolName: "t"}}
	var usage types.Usage
	_, err := executeToolsLimited(ctx, calls, tools, nil, nil, nil, &usage, toolCallEventCallbacks{toolExecutionMs: map[string]int64{}}, 1)
	if len(mustEvents(rec)) != 2 {
		t.Fatalf("events = %v, want only the first call started", mustEvents(rec))
	}
	_ = err
	// With concurrency > 1 the same guarantee holds for queued calls.
	rec2 := &concurrencyRecorder{}
	ctx2, cancel2 := context.WithCancel(context.Background())
	tools[0].Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		rec2.enter("t")
		cancel2()
		rec2.leave("t")
		return "ok", nil
	}
	_, err = executeToolsLimited(ctx2, calls, tools, nil, nil, nil, &usage, toolCallEventCallbacks{toolExecutionMs: map[string]int64{}}, 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if events := mustEvents(rec2); len(events) > 4 {
		t.Fatalf("events = %v, third call must not start", events)
	}
}

func mustEvents(r *concurrencyRecorder) []string { e, _ := r.snapshot(); return e }

// Ports collect-tool-approvals.test.ts 'should return approved calls in model
// order with split assistant messages' (TS #22135).
func TestCollectToolApprovals_ApprovedCallsInModelOrder(t *testing.T) {
	ids := []string{"10", "2", "1"}
	for _, split := range []bool{false, true} {
		var calls []types.ContentPart
		for _, id := range ids {
			calls = append(calls, types.ToolCallContent{ToolCallID: id, ToolName: "tool1", Input: "{}"})
		}
		var requests []types.ContentPart
		for i := len(ids) - 1; i >= 0; i-- {
			requests = append(requests, types.ToolApprovalRequestContent{ApprovalID: "approval-" + ids[i], ToolCallID: ids[i]})
		}
		var messages []types.Message
		if split {
			messages = append(messages,
				types.Message{Role: types.RoleAssistant, Content: calls[:1]},
				types.Message{Role: types.RoleAssistant, Content: append(append([]types.ContentPart(nil), calls[1:]...), requests...)},
			)
		} else {
			messages = append(messages, types.Message{Role: types.RoleAssistant, Content: append(append([]types.ContentPart(nil), calls...), requests...)})
		}
		var responses []types.ContentPart
		for i := len(ids) - 1; i >= 0; i-- {
			responses = append(responses, types.ToolApprovalResponseContent{ApprovalID: "approval-" + ids[i], Approved: true})
		}
		messages = append(messages, types.Message{Role: types.RoleTool, Content: responses})

		result, err := CollectToolApprovals(messages)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, a := range result.ApprovedToolApprovals {
			got = append(got, a.ToolCall.ID)
		}
		if !reflect.DeepEqual(got, ids) {
			t.Fatalf("split=%v: approved order = %v, want %v", split, got, ids)
		}
	}
}
