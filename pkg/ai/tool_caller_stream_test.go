package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports the "options.experimental_toolCallers" describe block from
// ai/src/generate-text/stream-text.test.ts (three cases: late-binds local
// caller and hides local-only callees; announces local caller tools in a
// message while preserving the caller definition; adds provider caller
// options while preserving direct access). See tool_caller_test.go for the
// generateText equivalents.

func TestStreamText_ToolCallers_LateBindsLocalCaller(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var modelTools []types.Tool

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			modelTools = opts.Tools
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID: "call-1", ToolName: "code_mode", Arguments: map[string]interface{}{},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	var toolResultOutput interface{}
	sawToolResult := false
	done := make(chan struct{})
	maxSteps := 1
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "Check inventory.",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{
			localCallerTool("code_mode", nil),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"sku": "sku-1", "availableUnits": 42}, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
		OnChunk: func(chunk provider.StreamChunk) {
			if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil && chunk.ToolResult.ToolName == "code_mode" {
				mu.Lock()
				sawToolResult = true
				toolResultOutput = chunk.ToolResult.Result
				mu.Unlock()
			}
		},
		OnFinish: func(*StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if got, want := toolNames(modelTools), []string{"code_mode"}; !equalStrings(got, want) {
		t.Fatalf("modelTools = %v, want %v", got, want)
	}
	if !sawToolResult {
		t.Fatalf("expected a tool-result chunk for code_mode")
	}
	names, ok := toolResultOutput.([]string)
	if !ok || len(names) != 1 || names[0] != "getInventory" {
		t.Fatalf("tool-result output = %v, want [getInventory]", toolResultOutput)
	}
}

func TestStreamText_ToolCallers_AnnouncesLocalCallerInMessage(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var modelTools []types.Tool
	var modelMessages []types.Message

	msg := "Available caller tools: getInventory."
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			modelTools = opts.Tools
			modelMessages = opts.Prompt.Messages
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	done := make(chan struct{})
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Check inventory.",
		Tools: []types.Tool{
			localCallerTool("code_mode", func(map[string]types.Tool) *string { return &msg }),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return nil, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
		OnFinish: func(*StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(modelTools) != 1 || modelTools[0].Name != "code_mode" {
		t.Fatalf("modelTools = %v, want only [code_mode]", toolNames(modelTools))
	}
	foundOriginal, foundCaller := false, false
	for _, m := range modelMessages {
		if m.Role != types.RoleUser || len(m.Content) != 1 {
			continue
		}
		tc, ok := m.Content[0].(types.TextContent)
		if !ok {
			continue
		}
		if tc.Text == "Check inventory." {
			foundOriginal = true
		}
		if tc.Text == msg {
			foundCaller = true
		}
	}
	if !foundOriginal || !foundCaller {
		t.Fatalf("expected both the prompt and caller message in prompt, got %+v", modelMessages)
	}
}

func TestStreamText_ToolCallers_AddsProviderCallerOptions(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var modelTools []types.Tool

	providerCaller := types.Tool{
		Name:       "programmatic",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type: types.ToolCallerTypeProvider,
			PrepareProviderOptions: func(providerOptions map[string]interface{}) map[string]interface{} {
				out := map[string]interface{}{}
				for k, v := range providerOptions {
					out[k] = v
				}
				out["test"] = map[string]interface{}{"allowedCallers": []string{"programmatic"}}
				return out
			},
		},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			modelTools = opts.Tools
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	done := make(chan struct{})
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Check demand.",
		Tools: []types.Tool{
			providerCaller,
			{
				Name:       "getDemand",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return nil, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getDemand": {DirectToolCall, "programmatic"},
		},
		OnFinish: func(*StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	var demand *types.Tool
	for i := range modelTools {
		if modelTools[i].Name == "getDemand" {
			demand = &modelTools[i]
		}
	}
	if demand == nil {
		t.Fatalf("getDemand not found in model tools: %v", toolNames(modelTools))
	}
	opts, ok := demand.ProviderOptions.(map[string]interface{})
	if !ok {
		t.Fatalf("getDemand.ProviderOptions = %v, want map", demand.ProviderOptions)
	}
	test, ok := opts["test"].(map[string]interface{})
	if !ok {
		t.Fatalf("getDemand.ProviderOptions[test] = %v", opts["test"])
	}
	callers, ok := test["allowedCallers"].([]string)
	if !ok || len(callers) != 1 || callers[0] != "programmatic" {
		t.Fatalf("allowedCallers = %v", test["allowedCallers"])
	}
}
