package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports the "experimental_toolCallers" describe block from
// ai/src/generate-text/generate-text.test.ts.

func localCallerTool(name string, prepareModelMessage func([]types.Tool) *string) types.Tool {
	return types.Tool{
		Name:       name,
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, errCallerNotBound
		},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type: types.ToolCallerTypeLocal,
			Bind: func(tools []types.Tool) types.Tool {
				return types.Tool{
					Name:       name,
					Parameters: map[string]interface{}{"type": "object"},
					Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
						names := make([]string, 0, len(tools))
						for _, tl := range tools {
							names = append(names, tl.Name)
						}
						return names, nil
					},
				}
			},
			PrepareModelMessage: func(tools []types.Tool) *string {
				if prepareModelMessage == nil {
					return nil
				}
				return prepareModelMessage(tools)
			},
		},
	}
}

var errCallerNotBound = &callerNotBoundError{}

type callerNotBoundError struct{}

func (*callerNotBoundError) Error() string { return "Caller was not bound." }

func toolCallResponse(id, name string, args map[string]interface{}) *types.GenerateResult {
	return &types.GenerateResult{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls: []types.ToolCall{
			{ID: id, ToolName: name, Arguments: args},
		},
	}
}

func TestGenerateText_ToolCallers_LateBindsLocalCaller(t *testing.T) {
	t.Parallel()
	var modelTools []types.Tool
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelTools = opts.Tools
			return toolCallResponse("call-1", "code_mode", nil), nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			localCallerTool("code_mode", nil),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"sku": input["sku"], "availableUnits": 42}, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if len(modelTools) != 1 || modelTools[0].Name != "code_mode" {
		t.Fatalf("modelTools = %v, want only [code_mode]", toolNames(modelTools))
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("toolResults = %d, want 1", len(result.ToolResults))
	}
	names, ok := result.ToolResults[0].Result.([]string)
	if !ok || len(names) != 1 || names[0] != "getInventory" {
		t.Fatalf("toolResults[0].Output = %v, want [getInventory]", result.ToolResults[0].Result)
	}
}

func TestGenerateText_ToolCallers_DoesNotExecuteLocalOnlyCalleesCalledDirectly(t *testing.T) {
	t.Parallel()
	executed := false
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return toolCallResponse("call-1", "getInventory", map[string]interface{}{"sku": "sku-1"}), nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			localCallerTool("code_mode", nil),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					executed = true
					return nil, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if executed {
		t.Fatalf("expected getInventory.Execute to not be called")
	}
	// getInventory is caller-only (not model-visible), so the direct call the
	// mock model emits is rejected as an unknown tool: Execute never runs,
	// and the synthesized ToolResult carries the NoSuchToolError instead of
	// a value (Go's ToolResult always has an entry per ToolCall; TS instead
	// leaves toolResults empty for an invalid call).
	if len(result.ToolResults) != 1 || result.ToolResults[0].Error == nil {
		t.Fatalf("toolResults = %+v, want one entry with a NoSuchToolError", result.ToolResults)
	}
	if len(result.ToolCalls) != 1 || !result.ToolCalls[0].Invalid {
		t.Fatalf("toolCalls = %+v, want one invalid call", result.ToolCalls)
	}
}

func TestGenerateText_ToolCallers_AnnouncesLocalCallerInMessage(t *testing.T) {
	t.Parallel()
	var modelTools []types.Tool
	var modelMessages []types.Message
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelTools = opts.Tools
			modelMessages = opts.Prompt.Messages
			return toolCallResponse("call-1", "code_mode", nil), nil
		},
	}

	msg := "Available caller tools: getInventory."
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			localCallerTool("code_mode", func(tools []types.Tool) *string { return &msg }),
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
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if len(modelTools) != 1 || modelTools[0].Name != "code_mode" {
		t.Fatalf("modelTools = %v", toolNames(modelTools))
	}
	foundOriginal := false
	foundCaller := false
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
	if len(result.ToolResults) != 1 {
		t.Fatalf("toolResults = %d, want 1", len(result.ToolResults))
	}
}

func TestGenerateText_ToolCallers_DoesNotRepeatCallerMessageAlreadyPresent(t *testing.T) {
	t.Parallel()
	var modelMessages []types.Message
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelMessages = opts.Prompt.Messages
			return &types.GenerateResult{FinishReason: types.FinishReasonStop}, nil
		},
	}

	catalog := "Current code mode catalog."
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Prompt."}}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: catalog}}},
		},
		Tools: []types.Tool{
			localCallerTool("code_mode", func([]types.Tool) *string { return &catalog }),
			{
				Name:       "nested",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return nil, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"nested": {"code_mode"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	count := 0
	for _, m := range modelMessages {
		if m.Role != types.RoleUser || len(m.Content) != 1 {
			continue
		}
		if tc, ok := m.Content[0].(types.TextContent); ok && tc.Text == catalog {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("catalog message appears %d times, want 1", count)
	}
}

func TestGenerateText_ToolCallers_AddsProviderCallerOptions(t *testing.T) {
	t.Parallel()
	var modelTools []types.Tool
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelTools = opts.Tools
			return &types.GenerateResult{FinishReason: types.FinishReasonStop}, nil
		},
	}

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

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Prompt:   "Check demand.",
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
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
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

func TestGenerateText_ToolCallers_DistinguishesDirectMarkerFromCallerNamedDirect(t *testing.T) {
	t.Parallel()
	var modelTools []types.Tool
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelTools = opts.Tools
			return toolCallResponse("call-1", "direct", nil), nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			localCallerTool("direct", nil),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return nil, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"direct", DirectToolCall},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got, want := toolNames(modelTools), []string{"direct", "getInventory"}; !equalStrings(got, want) {
		t.Fatalf("modelTools = %v, want %v", got, want)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("toolResults = %d, want 1", len(result.ToolResults))
	}
	names, ok := result.ToolResults[0].Result.([]string)
	if !ok || len(names) != 1 || names[0] != "getInventory" {
		t.Fatalf("toolResults[0].Output = %v, want [getInventory]", result.ToolResults[0].Result)
	}
}

func TestGenerateText_ToolCallers_RejectsInvalidCallerNames(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return nil, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"getInventory"},
		},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if want := `tool "getInventory" contains an invalid caller.`; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
