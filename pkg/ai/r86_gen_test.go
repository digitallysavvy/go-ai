package ai

import (
	"context"
	"sync"
	"testing"
	"time"

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

// r86genResumeHistory: assistant calls a (tool slow) and b (tool fast), each
// with an approval request, and a tool message approving both.
func r86genResumeHistory() []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "a", ToolName: "slow", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "ap-a", ToolCallID: "a"},
			types.ToolCallContent{ToolCallID: "b", ToolName: "fast", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "ap-b", ToolCallID: "b"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "ap-a", Approved: true},
			types.ToolApprovalResponseContent{ApprovalID: "ap-b", Approved: true},
		}},
	}
}

func r86genResumeTools() []types.Tool {
	return []types.Tool{
		{Name: "slow", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			time.Sleep(100 * time.Millisecond)
			return "slow", nil
		}},
		{Name: "fast", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "fast", nil
		}},
	}
}

func r86genToolMessageIDs(messages []types.Message) []string {
	var ids []string
	for _, m := range messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, p := range m.Content {
			if r, ok := p.(types.ToolResultContent); ok {
				ids = append(ids, r.ToolCallID)
			}
		}
		break
	}
	return ids
}

// cr-r86-stream-to-gen: StreamText's approval resume reports each approved
// tool's result as it settles (TS stream-text.ts 2341-2372, recorded with
// Node v24 streamText: tool parts b, a; responseMessages tool message b, a;
// the model prompt's tool message b, a). With ToolCallConcurrency 1 the
// calls run in order. GenerateText keeps call order (TS executeTools).
func TestR86genResumeApprovedToolsCompletionOrder(t *testing.T) {
	approval := map[string]types.ToolApprovalValue{"slow": types.ToolApprovalStatusUserApproval, "fast": types.ToolApprovalStatusUserApproval}
	run := func(t *testing.T, concurrency *int) (parts, response, prompt []string) {
		var mu sync.Mutex
		var prompts [][]types.Message
		model := &testutil.MockLanguageModel{DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			prompts = append(prompts, o.Prompt.Messages)
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
		res, err := StreamText(context.Background(), StreamTextOptions{
			Model: model, Messages: r86genResumeHistory(), Tools: r86genResumeTools(),
			ToolApproval: approval, ToolCallConcurrency: concurrency,
		})
		if err != nil {
			t.Fatal(err)
		}
		for chunk := range res.Chunks() {
			if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
				parts = append(parts, chunk.ToolResult.ToolCallID)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if len(prompts) == 0 {
			t.Fatal("model not called")
		}
		return parts, r86genToolMessageIDs(res.ResponseMessages()), r86genToolMessageIDs(prompts[0])
	}
	t.Run("concurrent", func(t *testing.T) {
		parts, response, prompt := run(t, nil)
		for name, got := range map[string][]string{"stream parts": parts, "responseMessages": response, "prompt": prompt} {
			if len(got) != 2 || got[0] != "b" || got[1] != "a" {
				t.Errorf("%s = %v, want [b a]", name, got)
			}
		}
	})
	t.Run("concurrency 1", func(t *testing.T) {
		one := 1
		parts, response, prompt := run(t, &one)
		for name, got := range map[string][]string{"stream parts": parts, "responseMessages": response, "prompt": prompt} {
			if len(got) != 2 || got[0] != "a" || got[1] != "b" {
				t.Errorf("%s = %v, want [a b]", name, got)
			}
		}
	})
	t.Run("generate keeps call order", func(t *testing.T) {
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Messages: r86genResumeHistory(), Tools: r86genResumeTools(), ToolApproval: approval,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := r86genToolMessageIDs(res.ResponseMessages); len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("responseMessages = %v, want [a b]", got)
		}
	})
}
