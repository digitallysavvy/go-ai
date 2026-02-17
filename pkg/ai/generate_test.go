package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateText_BasicPrompt(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if len(opts.Prompt.Messages) != 1 {
				t.Errorf("expected 1 message, got %d", len(opts.Prompt.Messages))
			}
			if opts.Prompt.Messages[0].Role != types.RoleUser {
				t.Errorf("expected user role, got %s", opts.Prompt.Messages[0].Role)
			}
			input, output, total := int64(5), int64(10), int64(15)
			return &types.GenerateResult{
				Text:         "Hello! I'm an AI assistant.",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello, how are you?",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Hello! I'm an AI assistant." {
		t.Errorf("unexpected text: %s", result.Text)
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Errorf("unexpected finish reason: %s", result.FinishReason)
	}
}

func TestGenerateText_MessagePrompt(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if len(opts.Prompt.Messages) != 2 {
				t.Errorf("expected 2 messages, got %d", len(opts.Prompt.Messages))
			}
			return &types.GenerateResult{
				Text:         "I can help with that!",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi!"}}},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "I can help with that!" {
		t.Errorf("unexpected text: %s", result.Text)
	}
}

func TestGenerateText_SystemMessage(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if opts.Prompt.System != "You are a helpful assistant." {
				t.Errorf("expected system message, got: %s", opts.Prompt.System)
			}
			return &types.GenerateResult{
				Text:         "Response with system context",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
		System: "You are a helpful assistant.",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Response with system context" {
		t.Errorf("unexpected text: %s", result.Text)
	}
}

func TestGenerateText_NilModel(t *testing.T) {
	t.Parallel()

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  nil,
		Prompt: "Hello",
	})

	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if err.Error() != "model is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateText_TemperatureParams(t *testing.T) {
	t.Parallel()

	temp := 0.7
	topP := 0.9
	topK := 50

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if opts.Temperature == nil || *opts.Temperature != temp {
				t.Errorf("expected temperature %f, got %v", temp, opts.Temperature)
			}
			if opts.TopP == nil || *opts.TopP != topP {
				t.Errorf("expected topP %f, got %v", topP, opts.TopP)
			}
			if opts.TopK == nil || *opts.TopK != topK {
				t.Errorf("expected topK %d, got %v", topK, opts.TopK)
			}
			return &types.GenerateResult{
				Text:         "response",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       model,
		Prompt:      "Hello",
		Temperature: &temp,
		TopP:        &topP,
		TopK:        &topK,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateText_MaxTokens(t *testing.T) {
	t.Parallel()

	maxTokens := 100

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if opts.MaxTokens == nil || *opts.MaxTokens != maxTokens {
				t.Errorf("expected maxTokens %d, got %v", maxTokens, opts.MaxTokens)
			}
			return &types.GenerateResult{
				Text:         "response",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "Hello",
		MaxTokens: &maxTokens,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateText_StopSequences(t *testing.T) {
	t.Parallel()

	stopSeqs := []string{"END", "STOP"}

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if len(opts.StopSequences) != 2 {
				t.Errorf("expected 2 stop sequences, got %d", len(opts.StopSequences))
			}
			return &types.GenerateResult{
				Text:         "response",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:         model,
		Prompt:        "Hello",
		StopSequences: stopSeqs,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateText_ToolCalling(t *testing.T) {
	t.Parallel()

	toolCalled := false
	tools := []types.Tool{
		{
			Name:        "get_weather",
			Description: "Get the weather for a location",
			Parameters:  map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				toolCalled = true
				return map[string]interface{}{"temperature": 72, "condition": "sunny"}, nil
			},
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				// First call: model requests tool use
				return &types.GenerateResult{
					Text:         "",
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{ID: "call_1", ToolName: "get_weather", Arguments: map[string]interface{}{"location": "NYC"}},
					},
				}, nil
			}
			// Second call: model uses tool result
			return &types.GenerateResult{
				Text:         "The weather in NYC is 72°F and sunny!",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "What's the weather in NYC?",
		Tools:    tools,
		StopWhen: []StopCondition{StepCountIs(5)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !toolCalled {
		t.Error("expected tool to be called")
	}
	if result.Text != "The weather in NYC is 72°F and sunny!" {
		t.Errorf("unexpected text: %s", result.Text)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
	if len(result.ToolResults) != 1 {
		t.Errorf("expected 1 tool result, got %d", len(result.ToolResults))
	}
}

func TestGenerateText_MultiStepToolCalling(t *testing.T) {
	t.Parallel()

	stepCount := 0
	tools := []types.Tool{
		{
			Name:        "step_tool",
			Description: "A tool that runs in steps",
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				stepCount++
				return map[string]interface{}{"step": stepCount}, nil
			},
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount < 3 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{ID: "call_" + string(rune('0'+callCount)), ToolName: "step_tool", Arguments: map[string]interface{}{}},
					},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "All steps complete!",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Run steps",
		Tools:    tools,
		StopWhen: []StopCondition{StepCountIs(5)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stepCount != 2 {
		t.Errorf("expected 2 tool calls, got %d", stepCount)
	}
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}
}

func TestGenerateText_ToolNotFound(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "existing_tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return nil, nil }},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{ID: "call_1", ToolName: "nonexistent_tool", Arguments: map[string]interface{}{}},
					},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "Done",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Call tool",
		Tools:  tools,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Tool not found should still create a result with an error in the tool result
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.ToolResults))
	}
	if result.ToolResults[0].Error == nil {
		t.Error("expected error in tool result for nonexistent tool")
	}
}

func TestGenerateText_ToolExecutionError(t *testing.T) {
	t.Parallel()

	toolError := errors.New("tool execution failed")
	tools := []types.Tool{
		{
			Name: "failing_tool",
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return nil, toolError
			},
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{ID: "call_1", ToolName: "failing_tool", Arguments: map[string]interface{}{}},
					},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "Handled error",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Call failing tool",
		Tools:  tools,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.ToolResults))
	}
	if result.ToolResults[0].Error == nil {
		t.Error("expected error in tool result")
	}
	if result.ToolResults[0].Error.Error() != "tool execution failed" {
		t.Errorf("unexpected error message: %v", result.ToolResults[0].Error)
	}
}

func TestGenerateText_MaxStepsLimit(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }},
	}

	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			// Always request tool call
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: "call_1", ToolName: "tool", Arguments: map[string]interface{}{}},
				},
			}, nil
		},
	}

	maxSteps := 3
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Loop forever",
		Tools:    tools,
		MaxSteps: &maxSteps,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Steps) != maxSteps {
		t.Errorf("expected %d steps (max), got %d", maxSteps, len(result.Steps))
	}
	// MaxSteps is sugar for StopWhen{StepCountIs(N)}, so StopReason should be set
	if result.StopReason != "maximum number of steps (3) reached" {
		t.Errorf("expected StopReason for MaxSteps, got %q", result.StopReason)
	}
}

func TestGenerateText_OnStepFinishCallback(t *testing.T) {
	t.Parallel()

	stepCallbacks := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "response",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnStepFinish: func(ctx context.Context, step types.StepResult, userContext interface{}) {
			stepCallbacks++
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stepCallbacks != 1 {
		t.Errorf("expected 1 step callback, got %d", stepCallbacks)
	}
}

func TestGenerateText_OnFinishCallback(t *testing.T) {
	t.Parallel()

	finishCalled := false
	var capturedResult *GenerateTextResult

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "response",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnFinish: func(ctx context.Context, result *GenerateTextResult, userContext interface{}) {
			finishCalled = true
			capturedResult = result
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !finishCalled {
		t.Error("expected OnFinish callback to be called")
	}
	if capturedResult == nil || capturedResult.Text != "response" {
		t.Error("callback did not receive correct result")
	}
}

func TestGenerateText_UsageTracking(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				input1, output1, total1 := int64(10), int64(5), int64(15)
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					Usage:        types.Usage{InputTokens: &input1, OutputTokens: &output1, TotalTokens: &total1},
					ToolCalls: []types.ToolCall{
						{ID: "1", ToolName: "tool", Arguments: map[string]interface{}{}},
					},
				}, nil
			}
			input, output, total := int64(20), int64(10), int64(30)
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total},
			}, nil
		},
	}

	tools := []types.Tool{
		{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Hello",
		Tools:    tools,
		StopWhen: []StopCondition{StepCountIs(5)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Usage should be accumulated
	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 30 {
		t.Errorf("expected 30 input tokens, got %v", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens == nil || *result.Usage.OutputTokens != 15 {
		t.Errorf("expected 15 output tokens, got %v", result.Usage.OutputTokens)
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 45 {
		t.Errorf("expected 45 total tokens, got %v", result.Usage.TotalTokens)
	}
}

func TestGenerateText_GenerationError(t *testing.T) {
	t.Parallel()

	expectedError := errors.New("API error")
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, expectedError
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedError) {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestBuildPrompt_TextPrompt(t *testing.T) {
	t.Parallel()

	prompt := buildPrompt("Hello", nil, "")

	if len(prompt.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(prompt.Messages))
	}
	if prompt.Messages[0].Role != types.RoleUser {
		t.Errorf("expected user role, got %s", prompt.Messages[0].Role)
	}
}

func TestBuildPrompt_MessagesPrompt(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}

	prompt := buildPrompt("", messages, "system")

	if len(prompt.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(prompt.Messages))
	}
	if prompt.System != "system" {
		t.Errorf("expected system message, got %s", prompt.System)
	}
}

func TestBuildPrompt_EmptyPrompt(t *testing.T) {
	t.Parallel()

	prompt := buildPrompt("", nil, "")

	if len(prompt.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(prompt.Messages))
	}
}

// --- StopWhen tests ---

func TestGenerateText_StopWhen_StepCountIs(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }},
	}

	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "tool", Arguments: map[string]interface{}{}}},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Loop",
		Tools:    tools,
		StopWhen: []StopCondition{StepCountIs(3)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}
	if result.StopReason == "" {
		t.Error("expected StopReason to be set")
	}
	if result.StopReason != "maximum number of steps (3) reached" {
		t.Errorf("unexpected StopReason: %s", result.StopReason)
	}
}

func TestGenerateText_StopWhen_CustomCondition(t *testing.T) {
	t.Parallel()

	callCount := 0
	tools := []types.Tool{
		{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }},
	}

	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			input := int64(100 * callCount)
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "tool", Arguments: map[string]interface{}{}}},
				Usage:        types.Usage{InputTokens: &input},
			}, nil
		},
	}

	// Stop when accumulated input tokens reach 300+
	tokenLimit := func(state StopConditionState) string {
		if state.Usage.GetInputTokens() >= 300 {
			return "token_limit_reached"
		}
		return ""
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Loop",
		Tools:    tools,
		StopWhen: []StopCondition{tokenLimit},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "token_limit_reached" {
		t.Errorf("expected token_limit_reached, got %q", result.StopReason)
	}
	// Usage per step: 100, 200, 300... (100*callCount)
	// Accumulated after step 2: 100+200=300, which triggers >= 300
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps to reach 300 tokens, got %d", len(result.Steps))
	}
}

func TestGenerateText_StopWhen_OverridesMaxSteps(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }},
	}

	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "tool", Arguments: map[string]interface{}{}}},
			}, nil
		},
	}

	maxSteps := 2
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Loop",
		Tools:    tools,
		MaxSteps: &maxSteps,
		StopWhen: []StopCondition{StepCountIs(5)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// StopWhen should take precedence: 5 steps, not 2
	if len(result.Steps) != 5 {
		t.Errorf("expected 5 steps (StopWhen wins over MaxSteps=2), got %d", len(result.Steps))
	}
}

func TestGenerateText_StopWhen_HasToolCall(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "search", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "result", nil }},
		{Name: "finish", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "done", nil }},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount < 3 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "search", Arguments: map[string]interface{}{}}},
				}, nil
			}
			// Step 3: call the "finish" tool
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call_2", ToolName: "finish", Arguments: map[string]interface{}{}}},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Do work then finish",
		Tools:    tools,
		StopWhen: []StopCondition{HasToolCall("finish")},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "tool 'finish' was called" {
		t.Errorf("expected HasToolCall stop reason, got %q", result.StopReason)
	}
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}
}

func TestGenerateText_StopWhen_NaturalStopFirst(t *testing.T) {
	t.Parallel()

	callCount := 0
	tools := []types.Tool{
		{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }},
	}

	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount < 3 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "tool", Arguments: map[string]interface{}{}}},
				}, nil
			}
			// Natural stop at step 3
			return &types.GenerateResult{
				Text:         "Done",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Loop",
		Tools:    tools,
		StopWhen: []StopCondition{StepCountIs(10)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "" {
		t.Errorf("expected empty StopReason for natural stop, got %q", result.StopReason)
	}
	if result.Text != "Done" {
		t.Errorf("expected 'Done', got %q", result.Text)
	}
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}
}

func TestGenerateText_DefaultStopWhen(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }},
	}

	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "tool", Arguments: map[string]interface{}{}}},
			}, nil
		},
	}

	// Neither MaxSteps nor StopWhen set -- should default to 1
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Loop",
		Tools:  tools,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Steps) != 1 {
		t.Errorf("expected 1 step (default), got %d", len(result.Steps))
	}
}
