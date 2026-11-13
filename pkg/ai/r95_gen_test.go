package ai

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var errR95genAbort = errors.New("r95gen abort reason")

// r95genToolCalls returns n tool calls of the "t" tool and the tool, whose
// Execute runs onExec with the call's id.
func r95genToolCalls(n int, onExec func(id string)) ([]types.ToolCall, []types.Tool) {
	calls := make([]types.ToolCall, n)
	for i := range calls {
		id := string(rune('a' + i))
		calls[i] = types.ToolCall{ID: id, ToolName: "t", Arguments: map[string]interface{}{}, RawArguments: `{}`}
	}
	tool := types.Tool{
		Name:       "t",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			onExec(o.ToolCallID)
			return "ok", nil
		},
	}
	return calls, []types.Tool{tool}
}

// TS mapWithConcurrency with a limit below the call count: every worker
// checks abortSignal.throwIfAborted() before it claims the next call, also
// after its last one, so an abort while the last call runs rejects the
// execution with signal.reason even though every call started. Without a
// limit (Promise.all) the signal is not checked.
func TestR95genToolLimitAbortAfterEveryCallStarted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   int
		wantErr bool
	}{
		{"limited", 2, true},
		{"sequential", 1, true},
		{"unlimited", 0, false},
		{"limit covers every call", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			var ran atomic.Int32
			calls, tools := r95genToolCalls(3, func(id string) {
				ran.Add(1)
				if id == "c" {
					cancel(errR95genAbort)
				}
			})
			var usage types.Usage
			results, err := executeToolsLimited(ctx, calls, tools, nil, nil, nil, &usage, toolCallEventCallbacks{}, tc.limit)
			if ran.Load() != 3 || len(results) != 3 {
				t.Fatalf("ran %d calls, %d results; want every call to run", ran.Load(), len(results))
			}
			if tc.wantErr && err != errR95genAbort {
				t.Fatalf("err = %v, want the abort reason", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want none (Promise.all does not check the signal)", err)
			}
		})
	}
}

// Through GenerateText: the rejected tool execution fails the open step
// (onStepError, no onStepEnd), as TS executeTools rejects inside the step.
func TestR95genGenerateTextToolLimitAbortFailsTheStep(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	calls, tools := r95genToolCalls(2, func(id string) {
		if id == "b" {
			cancel(errR95genAbort)
		}
	})
	content := make([]types.ContentPart, len(calls))
	for i, c := range calls {
		content[i] = types.ToolCallContent{ToolCallID: c.ID, ToolName: c.ToolName, Input: `{}`}
	}
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, Content: content, ToolCalls: calls}, nil
	}}
	one := 1
	var stepEnds, stepErrors int
	var stepErr error
	_, err := GenerateText(ctx, GenerateTextOptions{
		Model:               model,
		Prompt:              "go",
		Tools:               tools,
		ToolCallConcurrency: &one,
		StopWhen:            []StopCondition{StepCountIs(5)},
		OnStepEnd:           func(context.Context, types.StepResult, interface{}) { stepEnds++ },
		OnStepError: func(_ context.Context, e OnStepErrorEvent) {
			stepErrors++
			stepErr = e.Error
		},
	})
	if !errors.Is(err, errR95genAbort) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
	if stepEnds != 0 || stepErrors != 1 || !errors.Is(stepErr, errR95genAbort) {
		t.Fatalf("step ends %d, step errors %d (%v); want the step to fail with the abort reason", stepEnds, stepErrors, stepErr)
	}
}

// TS DecisionLanguageModel parses the joined text parts of result.content: a
// model that reports its text only as content text parts is answered.
func TestR95genDecisionLanguageModelReadsContentText(t *testing.T) {
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{FinishReason: types.FinishReasonStop, Content: []types.ContentPart{
			types.TextContent{Text: `{"q0":`}, types.TextContent{Text: `0.25}`},
		}}, nil
	}}
	dm, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: model})
	if err != nil {
		t.Fatal(err)
	}
	res, err := dm.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "state",
		Questions: map[string]provider.DecisionQuestion{"ok": {Type: "boolean", Instructions: "is it ok?"}},
	})
	if err != nil {
		t.Fatalf("DoDecide: %v", err)
	}
	if p := res.Answers["ok"].Probability; p == nil || *p != 0.25 {
		t.Fatalf("answer = %+v, want P(true) 0.25", res.Answers["ok"])
	}
}

// A language model that resolves without a result fails the decision (TS
// reads result.finishReason) instead of crashing it.
func TestR95genDecisionLanguageModelNoResult(t *testing.T) {
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return nil, nil
	}}
	dm, _ := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: model})
	_, err := dm.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "state",
		Questions: map[string]provider.DecisionQuestion{"ok": {Type: "boolean", Instructions: "is it ok?"}},
	})
	if err == nil || !strings.Contains(err.Error(), "returned no result") {
		t.Fatalf("err = %v, want the no-result error", err)
	}
}

// r95genNilBatch is a batch interface whose calls resolve without a result.
type r95genNilBatch struct{}

func (r95genNilBatch) SpecificationVersion() string       { return "v4" }
func (r95genNilBatch) Provider() string                   { return "nil.batch" }
func (r95genNilBatch) SupportedURLs() map[string][]string { return nil }
func (r95genNilBatch) DoStartBatch(context.Context, provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
	return nil, nil
}
func (r95genNilBatch) DoGetBatchStatus(context.Context, provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
	return nil, nil
}
func (r95genNilBatch) DoGetBatchResults(context.Context, provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
	return nil, nil
}
func (r95genNilBatch) DoCancelBatch(context.Context, provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
	return nil, nil
}
func (r95genNilBatch) DoListBatches(context.Context, provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error) {
	return nil, nil
}

// A batch provider call that resolves without a result fails the call (TS
// fails reading the missing result) instead of crashing it.
func TestR95genBatchProviderNoResult(t *testing.T) {
	ctx := context.Background()
	b := r95genNilBatch{}
	ref := BatchReference{Version: 2, ID: "b1", Provider: b.Provider()}
	wantNoResult := func(t *testing.T, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "returned no result") {
			t.Fatalf("err = %v, want the no-result error", err)
		}
	}
	t.Run("start", func(t *testing.T) {
		_, err := ExperimentalStartBatch(ctx, StartBatchOptions{Provider: b, Requests: []BatchRequest{{Text: &BatchTextRequest{ID: "r1", Model: "m", Prompt: "hi"}}}})
		wantNoResult(t, err)
	})
	t.Run("status", func(t *testing.T) {
		_, err := ExperimentalGetBatchStatus(ctx, GetBatchStatusOptions{Provider: b, Batch: ref})
		wantNoResult(t, err)
	})
	t.Run("results", func(t *testing.T) {
		_, err := ExperimentalGetBatchResults(ctx, GetBatchResultsOptions{Provider: b, Batch: ref})
		wantNoResult(t, err)
	})
	t.Run("list", func(t *testing.T) {
		_, err := ExperimentalListBatches(ctx, ListBatchesOptions{Provider: b})
		wantNoResult(t, err)
	})
	t.Run("cancel", func(t *testing.T) {
		res, err := ExperimentalCancelBatch(ctx, CancelBatchOptions{Provider: b, Batch: ref})
		if err != nil || res == nil {
			t.Fatalf("cancel = %v, %v; want an empty result (TS returns the provider's result)", res, err)
		}
	})
}
