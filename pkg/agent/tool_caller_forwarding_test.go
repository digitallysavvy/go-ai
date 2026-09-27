package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports "should forward experimental_toolCallers to generateText" and
// "should forward experimental_toolCallers to streamText" from
// ai/src/agent/tool-loop-agent.test.ts: AgentConfig.ExperimentalToolCallers
// (constructor level) must reach the underlying GenerateText/StreamText
// call, narrowing the model-visible tool set exactly like a direct
// GenerateText/StreamText call with the same option would.

func codeModeCaller() types.Tool {
	return types.Tool{
		Name:       "code_mode",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type: types.ToolCallerTypeLocal,
			Bind: func(tools map[string]types.Tool) types.Tool {
				names := make([]string, 0, len(tools))
				for n := range tools {
					names = append(names, n)
				}
				return types.Tool{
					Name:       "code_mode",
					Parameters: map[string]interface{}{"type": "object"},
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						return names, nil
					},
				}
			},
		},
	}
}

func getInventoryTool() types.Tool {
	return types.Tool{
		Name:       "getInventory",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"sku": map[string]interface{}{"type": "string"}}},
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"sku": input["sku"]}, nil
		},
	}
}

func TestToolLoopAgent_ForwardsExperimentalToolCallersToGenerate(t *testing.T) {
	mock := &mockLanguageModel{}
	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{codeModeCaller(), getInventoryTool()},
		ExperimentalToolCallers: ai.ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
	})

	_, err := agent.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello, world!"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(mock.options) == 0 {
		t.Fatal("model was not called")
	}
	got := mock.options[0].Tools
	if len(got) != 1 || got[0].Name != "code_mode" {
		names := make([]string, len(got))
		for i, tl := range got {
			names[i] = tl.Name
		}
		t.Fatalf("model tools = %v, want only [code_mode] (getInventory should be hidden, routed only through code_mode)", names)
	}
}

func TestToolLoopAgent_ForwardsExperimentalToolCallersToStream(t *testing.T) {
	var gotTools []types.Tool
	model := &functionalAgentLanguageModel{
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "unused", FinishReason: types.FinishReasonStop}, nil
		},
		doStream: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			gotTools = opts.Tools
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{codeModeCaller(), getInventoryTool()},
		ExperimentalToolCallers: ai.ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
	})

	result, err := agent.Stream(context.Background(), AgentStreamOptions{
		AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello, world!"},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	_ = result.Steps()

	if len(gotTools) != 1 || gotTools[0].Name != "code_mode" {
		names := make([]string, len(gotTools))
		for i, tl := range gotTools {
			names[i] = tl.Name
		}
		t.Fatalf("model tools = %v, want only [code_mode]", names)
	}
}
