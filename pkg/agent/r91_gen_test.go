package agent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 91 gen tests (TS 2ebc78b654).

// core1-D01 (agent half): the loop passes the last step's response to
// parseCompleteOutput (TS generate-text.ts:1808), so the
// NoObjectGeneratedError carries the model response.
func TestR91genAgentOutputParseErrorResponse(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:          []types.ContentPart{types.TextContent{Text: "not json"}},
			Text:             "not json",
			FinishReason:     types.FinishReasonStop,
			ResponseMetadata: &types.ResponseMetadata{ID: "resp-1", ModelID: "m-1"},
		}, nil
	}}
	a := NewToolLoopAgent(AgentConfig{
		Model:  model,
		Output: ai.ObjectOutput[map[string]interface{}](ai.ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})}),
	})
	_, err := a.Execute(context.Background(), "prompt")
	var noObj *ai.NoObjectGeneratedError
	if !errors.As(err, &noObj) {
		t.Fatalf("err = %v, want NoObjectGeneratedError", err)
	}
	if noObj.Response == nil || noObj.Response.ID != "resp-1" || noObj.Response.ModelID != "m-1" {
		t.Fatalf("Response = %+v, want id resp-1 / model m-1", noObj.Response)
	}
}

// core1-A01 (agent tool sites): a zero tool timeout gives a TimeoutError
// with its duration marked as set (ai.NewToolTimeoutError), the TS
// AbortSignal.timeout(0) reason.
func TestR91genAgentZeroToolTimeoutMarkedSet(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		calls++
		if calls > 1 {
			return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "done"}}, Text: "done", FinishReason: types.FinishReasonStop}, nil
		}
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "slow", Input: `{}`}},
			ToolCalls:    []types.ToolCall{{ID: "c", ToolName: "slow", Arguments: map[string]interface{}{}, RawArguments: `{}`}},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}}
	slow := types.Tool{Name: "slow", Parameters: map[string]interface{}{"type": "object"}, Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}}
	zero := time.Duration(0)
	var mu sync.Mutex
	var got error
	a := NewToolLoopAgent(AgentConfig{
		Model:   model,
		Tools:   []types.Tool{slow},
		Timeout: &ai.TimeoutConfig{ToolMs: &zero},
		OnToolError: func(_ types.ToolCall, err error) {
			mu.Lock()
			defer mu.Unlock()
			got = err
		},
	})
	if _, err := a.Execute(context.Background(), "prompt"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var te *ai.TimeoutError
	if !errors.As(got, &te) {
		t.Fatalf("tool error = %v, want *ai.TimeoutError", got)
	}
	if te.Reason != ai.TimeoutReasonTool || !reflect.ValueOf(te).Elem().FieldByName("timeoutSet").Bool() {
		t.Fatalf("tool timeout error = %+v, want its duration marked as set", te)
	}
}
