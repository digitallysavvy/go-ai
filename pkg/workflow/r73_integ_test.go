package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r73integReasoningFileModel answers its first call with a reasoning file and
// a tool call, then "done", recording every call's prompt.
type r73integReasoningFileModel struct {
	wfMockModel
	mu      sync.Mutex
	prompts [][]types.Message
}

func (m *r73integReasoningFileModel) record(opts *provider.GenerateOptions) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, append([]types.Message(nil), opts.Prompt.Messages...))
	return len(m.prompts)
}

func (m *r73integReasoningFileModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if m.record(opts) == 1 {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &types.ReasoningFileContent{MediaType: "image/png", Data: []byte("png")}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"value": "a"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

func (m *r73integReasoningFileModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.record(opts) == 1 {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ReasoningFileContent{MediaType: "image/png", Data: []byte("png")},
			},
			ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"value": "a"}}},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func r73integPartKinds(messages []types.Message) string {
	var kinds []string
	for _, m := range messages {
		for _, part := range m.Content {
			kinds = append(kinds, string(m.Role)+":"+part.ContentType())
		}
	}
	return strings.Join(kinds, ",")
}

// r73 carry CR-1: TS model-call-iterator.ts getAssistantMessageContent drops
// reasoning-file parts in stream mode (`case 'reasoning-file': if (mode !==
// 'generate') break;`): the next step's prompt, the step's response messages
// and onEnd's messages have none; the step content keeps the file. Generate
// mode keeps them in the prompt.
func TestR73Integ_WorkflowStreamOmitsReasoningFilesFromPrompt(t *testing.T) {
	tool := types.Tool{Name: "testTool", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}
	for _, streaming := range []bool{true, false} {
		model := &r73integReasoningFileModel{}
		var mu sync.Mutex
		var endMessages []types.Message
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model: model,
			Tools: []types.Tool{tool},
			OnEnd: func(_ context.Context, e ai.OnFinishEvent) {
				mu.Lock()
				defer mu.Unlock()
				endMessages = e.Messages
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var steps []types.StepResult
		if streaming {
			st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.ConsumeStream(); err != nil {
				t.Fatal(err)
			}
			steps = st.Steps()
		} else {
			res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "test"})
			if err != nil {
				t.Fatal(err)
			}
			steps = res.Steps
		}
		model.mu.Lock()
		prompts := model.prompts
		model.mu.Unlock()
		if len(prompts) != 2 || len(steps) != 2 {
			t.Fatalf("streaming=%v: %d model calls, %d steps, want 2", streaming, len(prompts), len(steps))
		}
		if len(steps[0].ReasoningFiles()) != 1 {
			t.Errorf("streaming=%v: step content reasoning files = %d, want 1", streaming, len(steps[0].ReasoningFiles()))
		}
		gotPrompt := r73integPartKinds(prompts[1])
		gotResponse := r73integPartKinds(steps[0].Response.Messages)
		mu.Lock()
		gotEnd := r73integPartKinds(endMessages)
		mu.Unlock()
		wantPrompt := "user:text,assistant:tool-call,tool:tool-result"
		if !streaming {
			wantPrompt = "user:text,assistant:reasoning-file,assistant:tool-call,tool:tool-result"
		}
		if gotPrompt != wantPrompt {
			t.Errorf("streaming=%v: second prompt = %s, want %s", streaming, gotPrompt, wantPrompt)
		}
		if streaming {
			if strings.Contains(gotResponse, "reasoning-file") || strings.Contains(gotEnd, "reasoning-file") {
				t.Errorf("stream: step response messages = %s, end messages = %s; want no reasoning-file", gotResponse, gotEnd)
			}
			if !strings.Contains(gotEnd, "assistant:tool-call") {
				t.Errorf("stream: end messages = %s", gotEnd)
			}
		}
	}
}
