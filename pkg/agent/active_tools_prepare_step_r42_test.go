package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r42AgentTools() []types.Tool {
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}
	return []types.Tool{
		{Name: "a", Description: "a", Parameters: map[string]interface{}{"type": "object"}, Execute: exec},
		{
			Name:       "b",
			Parameters: map[string]interface{}{"type": "object"},
			Execute:    exec,
			DescriptionFunc: func(_ context.Context, opts types.ToolDescriptionOptions) string {
				if s, ok := opts.Context.(string); ok {
					return "b:" + s
				}
				return "b:none"
			},
		},
	}
}

// r42PrepareStep mirrors a TS prepareStep that widens activeTools and swaps
// toolsContext. TS ToolLoopAgent hands the full tool set and activeTools to
// generateText/streamText, which filter and resolve descriptions per step.
func r42PrepareStep(_ context.Context, step ai.PrepareStepOptions) ai.PrepareStepOptions {
	return ai.PrepareStepOptions{
		ActiveTools:  []string{"b"},
		ToolsContext: map[string]interface{}{"b": "step"},
	}
}

func r42ToolSnapshot(tools []types.Tool) map[string]string {
	out := map[string]string{}
	for _, tool := range tools {
		out[tool.Name] = tool.Description
	}
	return out
}

func TestToolLoopAgentGenerate_PrepareStepActiveToolsUseFullToolSet(t *testing.T) {
	var mu sync.Mutex
	var seen []map[string]string
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			seen = append(seen, r42ToolSnapshot(opts.Tools))
			mu.Unlock()
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model:        model,
		Tools:        r42AgentTools(),
		ActiveTools:  []string{"a"},
		ToolsContext: map[string]interface{}{"b": "call"},
		PrepareStep:  r42PrepareStep,
	})
	if _, err := agent.Generate(context.Background(), AgentGenerateOptions{Prompt: "hi"}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	want := []map[string]string{{"b": "b:step"}}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("model tools = %v, want %v", seen, want)
	}
}

func TestToolLoopAgentStream_PrepareStepActiveToolsUseFullToolSet(t *testing.T) {
	var mu sync.Mutex
	var seen []map[string]string
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			seen = append(seen, r42ToolSnapshot(opts.Tools))
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model:        model,
		Tools:        r42AgentTools(),
		ActiveTools:  []string{"a"},
		ToolsContext: map[string]interface{}{"b": "call"},
		PrepareStep:  r42PrepareStep,
	})
	result, err := agent.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "hi"}})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []map[string]string{{"b": "b:step"}}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("model tools = %v, want %v", seen, want)
	}
}
