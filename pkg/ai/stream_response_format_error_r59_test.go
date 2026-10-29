package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r59FormatOutput fails to build its response format from call failAt on.
type r59FormatOutput struct {
	mu     sync.Mutex
	calls  int
	failAt int
}

func (o *r59FormatOutput) ResponseFormat(context.Context) (*provider.ResponseFormat, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	if o.calls >= o.failAt {
		return nil, errors.New("no format")
	}
	return &provider.ResponseFormat{Type: "text"}, nil
}

func (o *r59FormatOutput) parseCompleteOutput(context.Context, ParseCompleteOutputOptions) (interface{}, error) {
	return nil, nil
}

func (o *r59FormatOutput) parsePartialOutput(context.Context, ParsePartialOutputOptions) (interface{}, bool, error) {
	return nil, false, nil
}

// TS stream-language-model-call awaits output.responseFormat inside doStream,
// after onStepStart: a format that cannot be built fails that step's model
// call (onStepStart, then onStepError and an error part). Go failed the first
// step before onStepStart, and ignored the failure on a later step.
func TestR59StreamTextResponseFormatFailureFailsTheModelCall(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(map[int]string{1: "first step", 2: "later step"}[failAt], func(t *testing.T) {
			var mu sync.Mutex
			var starts, stepErrors, modelCalls int
			var stepErr error
			calls := 0
			res, err := StreamText(context.Background(), StreamTextOptions{
				Prompt:   "a",
				Output:   &r59FormatOutput{failAt: failAt},
				StopWhen: []StopCondition{IsStepCount(3)},
				Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}}},
				OnStepStart: func(context.Context, OnStepStartEvent) { mu.Lock(); starts++; mu.Unlock() },
				OnStepError: func(_ context.Context, e OnStepErrorEvent) {
					mu.Lock()
					stepErrors++
					stepErr = e.Error
					mu.Unlock()
				},
				Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					mu.Lock()
					modelCalls++
					mu.Unlock()
					calls++
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c", ToolName: "t", Arguments: map[string]interface{}{}}},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			parts, _ := r58FullStreamParts(t, res)
			if !strings.HasSuffix(parts, " error") {
				t.Fatalf("full stream = %q, want it to end with an error part", parts)
			}
			mu.Lock()
			defer mu.Unlock()
			if starts != failAt || stepErrors != 1 || stepErr == nil || stepErr.Error() != "no format" || modelCalls != failAt-1 {
				t.Fatalf("OnStepStart = %d, OnStepError = %d (%v), model calls = %d; want %d, 1 (no format), %d", starts, stepErrors, stepErr, modelCalls, failAt, failAt-1)
			}
		})
	}
}
