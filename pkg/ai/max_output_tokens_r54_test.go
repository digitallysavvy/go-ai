package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func isMaxOutputTokensError(err error) bool {
	var invalid *providererrors.InvalidArgumentError
	return errors.As(err, &invalid) && invalid.Field == "maxOutputTokens" && invalid.Message == "maxOutputTokens must be >= 1"
}

// TS prepareLanguageModelCallOptions rejects maxOutputTokens < 1 in
// generateText, streamText, generateObject, streamObject and batch requests,
// and prepareStepCallSettings rejects a prepareStep override below 1.
func TestMaxOutputTokensBelowOneIsRejected(t *testing.T) {
	zero := 0
	calls := 0
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			return &types.GenerateResult{Text: "x", FinishReason: types.FinishReasonStop}, nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", MaxTokens: &zero}); !isMaxOutputTokensError(err) {
		t.Fatalf("GenerateText err = %v", err)
	}
	if _, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxTokens: &zero}); !isMaxOutputTokensError(err) {
		t.Fatalf("StreamText err = %v", err)
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "hi", Schema: testSchema, MaxTokens: &zero}); !isMaxOutputTokensError(err) {
		t.Fatalf("GenerateObject err = %v", err)
	}
	if _, err := StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "hi", Schema: testSchema, MaxTokens: &zero}); !isMaxOutputTokensError(err) {
		t.Fatalf("StreamObject err = %v", err)
	}
	req := textBatchRequest("req-1", "m")
	req.Text.MaxTokens = &zero
	if _, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{Provider: &mockBatchV4{providerName: "mock.batch"}, Requests: []BatchRequest{req}}); !isMaxOutputTokensError(err) {
		t.Fatalf("ExperimentalStartBatch err = %v", err)
	}

	prepare := func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
		return PrepareStepOptions{MaxOutputTokens: &zero}
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", PrepareStep: prepare}); !isMaxOutputTokensError(err) {
		t.Fatalf("GenerateText PrepareStep err = %v", err)
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", PrepareStep: prepare})
	if err == nil {
		_, err = res.ReadAll()
	}
	if !isMaxOutputTokensError(err) {
		t.Fatalf("StreamText PrepareStep err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("model called %d times", calls)
	}
}

// A later step's PrepareStep override below 1 fails that step (TS
// prepareStepCallSettings inside streamStep).
func TestStreamTextLaterStepMaxOutputTokensOverrideIsRejected(t *testing.T) {
	zero := 0
	streams := 0
	call := types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}, RawArguments: "{}"}
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				streams++
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &call},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			},
		},
		Prompt:   "go",
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{IsStepCount(3)},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber == 1 {
				return PrepareStepOptions{MaxOutputTokens: &zero}
			}
			return PrepareStepOptions{}
		},
	})
	if err == nil {
		_, err = res.ReadAll()
	}
	if !isMaxOutputTokensError(err) {
		t.Fatalf("err = %v", err)
	}
	if streams != 1 {
		t.Fatalf("model streamed %d times, want 1", streams)
	}
}
