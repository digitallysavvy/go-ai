package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r86genImageBatches records the "image" content part deprecation batches
// logged without provider or model (TS convertToLanguageModelPrompt's
// logWarnings({ warnings })), one entry per batch: the batch size.
func r86genImageBatches(t *testing.T) *[]int {
	t.Helper()
	var batches []int
	SetLogWarnings(func(o LogWarningsOptions) {
		if len(o.Warnings) > 0 && o.Warnings[0].Setting == `"image" content part` && o.Provider == "" && o.Model == "" {
			batches = append(batches, len(o.Warnings))
		}
	})
	t.Cleanup(func() { SetLogWarnings(nil) })
	return &batches
}

func r86genImageMessage(images int) types.Message {
	content := []types.ContentPart{types.TextContent{Text: "look"}}
	for i := 0; i < images; i++ {
		content = append(content, types.ImageContent{Image: []byte{byte(i + 1)}, MimeType: "image/png"})
	}
	return types.Message{Role: types.RoleUser, Content: content}
}

// V061 item 20: TS converts every step's messages (convertToLanguageModelPrompt
// in each step), so a user message with "image" parts that PrepareStep or a
// steering message adds is logged on the steps whose prompt contains it, and
// the initial prompt's image message is logged on every step that still
// sends it.
func TestR86genImageDeprecationFollowsStepMessages(t *testing.T) {
	t.Run("steering", func(t *testing.T) {
		batches := r86genImageBatches(t)
		steering := NewSteering()
		calls := 0
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				calls++
				if calls == 1 {
					// Delivered at the next step boundary.
					if _, err := steering.SteerMessage(r86genImageMessage(2)); err != nil {
						t.Fatal(err)
					}
				}
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			},
		}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:                model,
			Messages:             []types.Message{r86genImageMessage(1)},
			ExperimentalSteering: steering,
			StopWhen:             []StopCondition{StepCountIs(2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		// Step 1: the prompt's message (1 image). Step 2: the prompt's
		// message, then the steering message (2 images).
		if got := *batches; len(got) != 3 || got[0] != 1 || got[1] != 1 || got[2] != 2 {
			t.Fatalf("batches = %v, want [1 1 2]", got)
		}
	})

	t.Run("prepareStep", func(t *testing.T) {
		batches := r86genImageBatches(t)
		calls := 0
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				calls++
				if calls == 1 {
					return &types.GenerateResult{
						ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
						FinishReason: types.FinishReasonToolCalls,
					}, nil
				}
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			},
		}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:    model,
			Messages: []types.Message{r86genImageMessage(1)},
			Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
			PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
				if step.StepNumber == 1 {
					// Replace the prompt with a new image message.
					step.Messages = append([]types.Message{r86genImageMessage(3)}, step.Messages[1:]...)
				}
				return step
			},
			StopWhen: []StopCondition{StepCountIs(2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		// Step 1: the prompt's message. Step 2: only PrepareStep's message.
		if got := *batches; len(got) != 2 || got[0] != 1 || got[1] != 3 {
			t.Fatalf("batches = %v, want [1 3]", got)
		}
	})
}
