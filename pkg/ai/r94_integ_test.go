package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var errR94integAbortReason = errors.New("r94integ: caller reason")

// TS abortSignal?.throwIfAborted() throws signal.reason: a context cancelled
// with a cause reports the cause (context.Cause), not context.Canceled.
func TestR94integThrowIfAbortedReason(t *testing.T) {
	cancelled := func() context.Context {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errR94integAbortReason)
		return ctx
	}

	decision := &mockDecisionModel{
		providerName: "test",
		modelID:      "test-model",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
		},
	}
	if _, err := ExperimentalDecide(cancelled(), DecideOptions{Model: decision, State: "refund", Questions: decisionQuestionsFixture()}); !errors.Is(err, errR94integAbortReason) {
		t.Errorf("ExperimentalDecide before the call: err = %v, want the reason", err)
	}
	// After the model call.
	ctx, cancel := context.WithCancelCause(context.Background())
	after := &mockDecisionModel{
		providerName: "test",
		modelID:      "test-model",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			cancel(errR94integAbortReason)
			return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
		},
	}
	retries := 0
	if _, err := ExperimentalDecide(ctx, DecideOptions{Model: after, State: "refund", Questions: decisionQuestionsFixture(), MaxRetries: &retries}); !errors.Is(err, errR94integAbortReason) {
		t.Errorf("ExperimentalDecide after the call: err = %v, want the reason", err)
	}

	lm, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"q0":0.25}`, FinishReason: types.FinishReasonStop}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	opts := provider.DecisionCallOptions{State: "x", Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "?"}}}
	if _, err := lm.DoDecide(cancelled(), opts); !errors.Is(err, errR94integAbortReason) {
		t.Errorf("DecisionLanguageModel: err = %v, want the reason", err)
	}

	batch := &mockBatchV4{providerName: "mock.batch"}
	if _, err := ExperimentalStartBatch(cancelled(), StartBatchOptions{Provider: batch, Requests: []BatchRequest{textBatchRequest("req-1", "m")}}); !errors.Is(err, errR94integAbortReason) {
		t.Errorf("ExperimentalStartBatch: err = %v, want the reason", err)
	}
}

// TestR94integToolAbortReasons: TS tool-call-concurrency and parse-tool-call
// abortSignal?.throwIfAborted() throw signal.reason.
func TestR94integToolAbortReasons(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	tools := []types.Tool{{
		Name:       "t",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel(errR94integAbortReason)
			return "ok", nil
		},
	}}
	calls := []types.ToolCall{{ID: "1", ToolName: "t"}, {ID: "2", ToolName: "t"}}
	var usage types.Usage
	if _, err := executeToolsLimited(ctx, calls, tools, nil, nil, nil, &usage, toolCallEventCallbacks{toolExecutionMs: map[string]int64{}}, 1); !errors.Is(err, errR94integAbortReason) {
		t.Errorf("queued calls after the abort: err = %v, want the reason", err)
	}

	ctx2, cancel2 := context.WithCancelCause(context.Background())
	refiners := map[string]ToolInputRefiner{"t": func(context.Context, ToolInputRefinementOptions) (map[string]interface{}, error) {
		cancel2(errR94integAbortReason)
		return nil, errors.New("refine failed")
	}}
	if _, err := RefineToolCalls(ctx2, []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}, tools, refiners, nil, nil); !errors.Is(err, errR94integAbortReason) {
		t.Errorf("refine aborted: err = %v, want the reason", err)
	}
}

// TestR94integStreamTextAbortDuringToolsReason: TS stream-text
// rejectResultPromises(abortSignal.reason): a cancel with a cause while the
// tools run ends the stream with the cause.
func TestR94integStreamTextAbortDuringToolsReason(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}}
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		cancel(errR94integAbortReason)
		return "ok", nil
	}}
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{tool}, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Text()
	if !errors.Is(r.Err(), errR94integAbortReason) {
		t.Fatalf("Err() = %v, want the abort reason", r.Err())
	}
}
