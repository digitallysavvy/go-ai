package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS stream-language-model-call.ts converts each step's own messages
// (prepareStep's messages included), so the "image" part deprecation follows
// them. Recorded with streamText under Node v24 (packages/ai dist at
// 30548ee7f9): step 1 logs one batch of one warning (the initial prompt);
// step 2, whose prepareStep appends a user message with two image parts,
// logs that batch again and then a batch of two.
func TestR86streamImageDeprecationFollowsStepMessages(t *testing.T) {
	var batches []int
	SetLogWarnings(func(o LogWarningsOptions) {
		if len(o.Warnings) > 0 && o.Warnings[0].Setting == `"image" content part` {
			batches = append(batches, len(o.Warnings))
		}
	})
	defer SetLogWarnings(nil)

	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		batches = append(batches, -calls)
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "1", Text: "hi"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
		}), nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.ImageContent{Image: []byte{1, 2}, MimeType: "image/png"},
		}}},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return 1, nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber != 1 {
				return PrepareStepOptions{}
			}
			return PrepareStepOptions{Messages: append(append([]types.Message(nil), step.Messages...), types.Message{
				Role: types.RoleUser, Content: []types.ContentPart{
					types.ImageContent{Image: []byte{3}, MimeType: "image/png"},
					types.ImageContent{Image: []byte{4}, MimeType: "image/png"},
				}})}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAll(); err != nil {
		t.Fatal(err)
	}
	want := []int{1, -1, 1, 2, -2}
	if len(batches) != len(want) {
		t.Fatalf("log sequence = %v, want %v", batches, want)
	}
	for i := range want {
		if batches[i] != want[i] {
			t.Fatalf("log sequence = %v, want %v", batches, want)
		}
	}
}
