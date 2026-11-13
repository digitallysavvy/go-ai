package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// cr-r86-gen-to-stream via cr-r86-stream-to-integrator 1: StreamText logs the
// image deprecation of each step's own messages (TS
// stream-language-model-call.ts runs convertToLanguageModelPrompt per step):
// a steering message's parts on the steps that send it, and nothing for a
// prompt message PrepareStep removed. StreamText twin of
// TestR86genImageDeprecationFollowsStepMessages.
func TestR86integStreamTextImageDeprecationFollowsStepMessages(t *testing.T) {
	read := func(t *testing.T, opts StreamTextOptions) {
		t.Helper()
		res, err := StreamText(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("steering", func(t *testing.T) {
		batches := r86genImageBatches(t)
		steering := NewSteering()
		calls := 0
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				if _, err := steering.SteerMessage(r86genImageMessage(2)); err != nil {
					t.Fatal(err)
				}
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
		read(t, StreamTextOptions{Model: model, Messages: []types.Message{r86genImageMessage(1)}, ExperimentalSteering: steering, StopWhen: []StopCondition{StepCountIs(2)}})
		if got := *batches; len(got) != 3 || got[0] != 1 || got[1] != 1 || got[2] != 2 {
			t.Fatalf("batches = %v, want [1 1 2]", got)
		}
	})
	t.Run("prepareStep", func(t *testing.T) {
		batches := r86genImageBatches(t)
		calls := 0
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", RawArguments: "{}"}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
		read(t, StreamTextOptions{
			Model:    model,
			Messages: []types.Message{r86genImageMessage(1)},
			Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
			PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
				if step.StepNumber == 1 {
					step.Messages = append([]types.Message{r86genImageMessage(3)}, step.Messages[1:]...)
				}
				return step
			},
			StopWhen: []StopCondition{StepCountIs(2)},
		})
		if got := *batches; len(got) != 2 || got[0] != 1 || got[1] != 3 {
			t.Fatalf("batches = %v, want [1 3]", got)
		}
	})
}
