package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Helper function to create int64 pointers
func intPtr(i int64) *int64 {
	return &i
}

// Mock language model for testing
type mockLanguageModel struct {
	responses []types.GenerateResult
	callCount int
}

func (m *mockLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.callCount >= len(m.responses) {
		return &types.GenerateResult{
			Text:         "Final response",
			FinishReason: types.FinishReasonStop,
			Usage: types.Usage{
				TotalTokens: intPtr(10),
			},
		}, nil
	}

	result := m.responses[m.callCount]
	m.callCount++
	return &result, nil
}

func (m *mockLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, fmt.Errorf("streaming not implemented in mock")
}

func (m *mockLanguageModel) SpecificationVersion() string { return "v3" }
func (m *mockLanguageModel) Provider() string              { return "mock" }
func (m *mockLanguageModel) ModelID() string               { return "mock-model" }
func (m *mockLanguageModel) SupportsTools() bool           { return true }
func (m *mockLanguageModel) SupportsStructuredOutput() bool { return false }
func (m *mockLanguageModel) DefaultObjectGenerationMode() string { return "" }
func (m *mockLanguageModel) SupportsImageUrls() bool       { return false }
func (m *mockLanguageModel) SupportsImageInput() bool      { return false }
func (m *mockLanguageModel) SupportsParallelToolCalls() bool { return true }

// Test that OnStepFinish callback is called at constructor level
func TestOnStepFinish_ConstructorLevel(t *testing.T) {
	callCount := 0
	var capturedSteps []types.StepResult

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Step 1",
				FinishReason: types.FinishReasonStop,
				Usage: types.Usage{
					TotalTokens: intPtr(15),
				},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnStepFinish: func(step types.StepResult) {
			callCount++
			capturedSteps = append(capturedSteps, step)
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test prompt")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if callCount == 0 {
		t.Error("OnStepFinish was not called")
	}

	if len(capturedSteps) == 0 {
		t.Fatal("No steps were captured")
	}

	// Verify step result structure
	step := capturedSteps[0]
	if step.StepNumber != 1 {
		t.Errorf("Expected step number 1, got %d", step.StepNumber)
	}
	if step.Text != "Step 1" {
		t.Errorf("Expected text 'Step 1', got '%s'", step.Text)
	}
	if step.Usage.TotalTokens == nil || *step.Usage.TotalTokens != 15 {
		t.Errorf("Expected 15 total tokens, got %v", step.Usage.TotalTokens)
	}
	if step.FinishReason != types.FinishReasonStop {
		t.Errorf("Expected finish reason 'stop', got '%s'", step.FinishReason)
	}
}

// Test that OnStepFinish is called for each step in multi-step execution
func TestOnStepFinish_MultiStep(t *testing.T) {
	var steps []types.StepResult

	// Create a tool for multi-step execution
	testTool := types.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  map[string]interface{}{},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "Tool result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Let me use the tool",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{
						ID:       "call_1",
						ToolName: "test_tool",
						Arguments: map[string]interface{}{},
					},
				},
				Usage: types.Usage{
					TotalTokens: intPtr(15),
				},
			},
			{
				Text:         "Tool completed",
				FinishReason: types.FinishReasonStop,
				Usage: types.Usage{
					TotalTokens: intPtr(30),
				},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnStepFinish: func(step types.StepResult) {
			steps = append(steps, step)
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test prompt")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(steps) < 2 {
		t.Errorf("Expected at least 2 steps, got %d", len(steps))
	}

	// Verify first step has tool calls
	if len(steps[0].ToolCalls) == 0 {
		t.Error("First step should have tool calls")
	}

	// Verify step numbers are sequential
	for i, step := range steps {
		expectedStepNum := i + 1
		if step.StepNumber != expectedStepNum {
			t.Errorf("Step %d: expected step number %d, got %d", i, expectedStepNum, step.StepNumber)
		}
	}
}

// Test that returning an error from OnStepFinish stops execution
func TestOnStepFinish_ErrorHandling(t *testing.T) {
	stepCount := 0

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Step 1",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{
						ID:       "call_1",
						ToolName: "test_tool",
						Arguments: map[string]interface{}{},
					},
				},
				Usage: types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Step 2",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{
						ID:       "call_2",
						ToolName: "test_tool",
						Arguments: map[string]interface{}{},
					},
				},
				Usage: types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Step 3",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  map[string]interface{}{},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnStepFinish: func(step types.StepResult) {
			stepCount++
			// Note: In Go, OnStepFinish doesn't return error in current implementation
			// This test documents the current behavior
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// With current implementation, all steps should complete
	if stepCount < 2 {
		t.Errorf("Expected at least 2 steps, got %d", stepCount)
	}
}

// Test that StepResult contains all required fields
func TestStepResult_Structure(t *testing.T) {
	var capturedStep types.StepResult

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  map[string]interface{}{},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "Tool executed", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Using tool",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{
						ID:       "call_1",
						ToolName: "test_tool",
						Arguments: map[string]interface{}{},
					},
				},
				Usage: types.Usage{
					TotalTokens: intPtr(23),
				},
				Warnings: []types.Warning{
					{
						Type:    "test_warning",
						Message: "This is a test warning",
					},
				},
			},
			{
				Text:         "Done",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnStepFinish: func(step types.StepResult) {
			if step.StepNumber == 1 {
				capturedStep = step
			}
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify all fields are present
	if capturedStep.StepNumber != 1 {
		t.Errorf("Expected step number 1, got %d", capturedStep.StepNumber)
	}

	if capturedStep.Text == "" {
		t.Error("Expected text to be set")
	}

	if capturedStep.Usage.TotalTokens == nil || *capturedStep.Usage.TotalTokens == 0 {
		t.Error("Expected usage to be populated")
	}

	if capturedStep.FinishReason == "" {
		t.Error("Expected finish reason to be set")
	}

	if len(capturedStep.ToolCalls) == 0 {
		t.Error("Expected tool calls to be present")
	}

	if len(capturedStep.Warnings) == 0 {
		t.Error("Expected warnings to be present")
	}

	if len(capturedStep.ResponseMessages) == 0 {
		t.Error("Expected response messages to be present")
	}
}

// Test that OnStepFinish works with no tool calls
func TestOnStepFinish_NoToolCalls(t *testing.T) {
	callCount := 0

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Simple response",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnStepFinish: func(step types.StepResult) {
			callCount++
			if len(step.ToolCalls) != 0 {
				t.Error("Expected no tool calls")
			}
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "simple prompt")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected OnStepFinish to be called once, got %d", callCount)
	}
}

// Test OnStepFinish with nil callback (should not panic)
func TestOnStepFinish_NilCallback(t *testing.T) {
	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Response",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model:        mock,
		OnStepFinish: nil, // Explicitly nil
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute should not fail with nil callback: %v", err)
	}
}

// Test that ResponseMessages are populated correctly
func TestOnStepFinish_ResponseMessages(t *testing.T) {
	var capturedStep types.StepResult

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Hello world",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnStepFinish: func(step types.StepResult) {
			capturedStep = step
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(capturedStep.ResponseMessages) == 0 {
		t.Fatal("Expected response messages to be populated")
	}

	msg := capturedStep.ResponseMessages[0]
	if msg.Role != types.RoleAssistant {
		t.Errorf("Expected assistant role, got %s", msg.Role)
	}

	if len(msg.Content) == 0 {
		t.Error("Expected message content to be populated")
	}

	// Check that text content is present
	hasText := false
	for _, content := range msg.Content {
		if tc, ok := content.(types.TextContent); ok {
			if tc.Text == "Hello world" {
				hasText = true
			}
		}
	}
	if !hasText {
		t.Error("Expected text content to match generated text")
	}
}
// =============================================================================
// LangChain-Style Callbacks Tests (v6.0.60+)
// =============================================================================

// Test OnChainStart callback
func TestOnChainStart(t *testing.T) {
	called := false
	var capturedInput string
	var capturedMessages []types.Message

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Response",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnChainStart: func(input string, messages []types.Message) {
			called = true
			capturedInput = input
			capturedMessages = messages
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test prompt")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnChainStart was not called")
	}

	if capturedInput != "test prompt" {
		t.Errorf("Expected input 'test prompt', got '%s'", capturedInput)
	}

	if len(capturedMessages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(capturedMessages))
	}
}

// Test OnChainEnd callback
func TestOnChainEnd(t *testing.T) {
	called := false
	var capturedResult *AgentResult

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Final answer",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(20)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnChainEnd: func(result *AgentResult) {
			called = true
			capturedResult = result
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnChainEnd was not called")
	}

	if capturedResult == nil {
		t.Fatal("capturedResult is nil")
	}

	if capturedResult.Text != "Final answer" {
		t.Errorf("Expected text 'Final answer', got '%s'", capturedResult.Text)
	}

	if len(capturedResult.Steps) != 1 {
		t.Errorf("Expected 1 step, got %d", len(capturedResult.Steps))
	}
}

// mockErrorModel is a model that always returns an error
type mockErrorModel struct{}

func (m *mockErrorModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	return nil, fmt.Errorf("simulated model error")
}

func (m *mockErrorModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, fmt.Errorf("streaming not implemented")
}

func (m *mockErrorModel) SpecificationVersion() string       { return "v3" }
func (m *mockErrorModel) Provider() string                    { return "mock-error" }
func (m *mockErrorModel) ModelID() string                     { return "error-model" }
func (m *mockErrorModel) SupportsTools() bool                 { return true }
func (m *mockErrorModel) SupportsStructuredOutput() bool      { return false }
func (m *mockErrorModel) DefaultObjectGenerationMode() string { return "" }
func (m *mockErrorModel) SupportsImageUrls() bool             { return false }
func (m *mockErrorModel) SupportsImageInput() bool            { return false }
func (m *mockErrorModel) SupportsParallelToolCalls() bool     { return true }

// Test OnChainError callback
func TestOnChainError(t *testing.T) {
	called := false
	var capturedError error

	mock := &mockErrorModel{}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnChainError: func(err error) {
			called = true
			capturedError = err
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")

	// Expect an error from the model
	if err == nil {
		t.Fatal("Expected an error but got nil")
	}

	if !called {
		t.Error("OnChainError was not called")
	}

	if capturedError == nil {
		t.Error("capturedError is nil")
	}
}

// Test OnAgentAction callback
func TestOnAgentAction(t *testing.T) {
	callCount := 0
	var capturedActions []AgentAction

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "tool result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Using tool",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_1",
						ToolName:  "test_tool",
						Arguments: map[string]interface{}{"arg": "value"},
					},
				},
				Usage: types.Usage{TotalTokens: intPtr(15)},
			},
			{
				Text:         "Done",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnAgentAction: func(action AgentAction) {
			callCount++
			capturedActions = append(capturedActions, action)
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected OnAgentAction to be called once, got %d", callCount)
	}

	if len(capturedActions) != 1 {
		t.Fatalf("Expected 1 captured action, got %d", len(capturedActions))
	}

	action := capturedActions[0]
	if action.ToolCall.ToolName != "test_tool" {
		t.Errorf("Expected tool name 'test_tool', got '%s'", action.ToolCall.ToolName)
	}

	if action.StepNumber != 1 {
		t.Errorf("Expected step number 1, got %d", action.StepNumber)
	}

	if action.Reasoning != "Using tool" {
		t.Errorf("Expected reasoning 'Using tool', got '%s'", action.Reasoning)
	}
}

// Test OnAgentFinish callback
func TestOnAgentFinish(t *testing.T) {
	called := false
	var capturedFinish AgentFinish

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Final answer",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnAgentFinish: func(finish AgentFinish) {
			called = true
			capturedFinish = finish
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnAgentFinish was not called")
	}

	if capturedFinish.Output != "Final answer" {
		t.Errorf("Expected output 'Final answer', got '%s'", capturedFinish.Output)
	}

	if capturedFinish.StepNumber != 1 {
		t.Errorf("Expected step number 1, got %d", capturedFinish.StepNumber)
	}

	if capturedFinish.FinishReason != types.FinishReasonStop {
		t.Errorf("Expected finish reason stop, got %s", capturedFinish.FinishReason)
	}

	if capturedFinish.Metadata == nil {
		t.Error("Expected metadata to be populated")
	}
}

// Test OnAgentFinish with max steps
func TestOnAgentFinish_MaxSteps(t *testing.T) {
	called := false
	var capturedFinish AgentFinish

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Step 1",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "test_tool", Arguments: map[string]interface{}{}}},
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Step 2",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "2", ToolName: "test_tool", Arguments: map[string]interface{}{}}},
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    mock,
		Tools:    []types.Tool{testTool},
		MaxSteps: 2,
		OnAgentFinish: func(finish AgentFinish) {
			called = true
			capturedFinish = finish
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnAgentFinish was not called when max steps reached")
	}

	if capturedFinish.FinishReason != types.FinishReasonLength {
		t.Errorf("Expected finish reason 'length', got '%s'", capturedFinish.FinishReason)
	}

	// Check that max_steps_hit metadata is set
	if maxStepsHit, ok := capturedFinish.Metadata["max_steps_hit"].(bool); !ok || !maxStepsHit {
		t.Error("Expected max_steps_hit metadata to be true")
	}
}

// Test OnToolStart callback
func TestOnToolStart(t *testing.T) {
	called := false
	var capturedToolCall types.ToolCall

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: "call_1", ToolName: "test_tool", Arguments: map[string]interface{}{}},
				},
				Usage: types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Done",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnToolStart: func(toolCall types.ToolCall) {
			called = true
			capturedToolCall = toolCall
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnToolStart was not called")
	}

	if capturedToolCall.ToolName != "test_tool" {
		t.Errorf("Expected tool name 'test_tool', got '%s'", capturedToolCall.ToolName)
	}
}

// Test OnToolEnd callback
func TestOnToolEnd(t *testing.T) {
	called := false
	var capturedResult types.ToolResult

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "success result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: "call_1", ToolName: "test_tool", Arguments: map[string]interface{}{}},
				},
				Usage: types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Done",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnToolEnd: func(toolResult types.ToolResult) {
			called = true
			capturedResult = toolResult
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnToolEnd was not called")
	}

	if capturedResult.ToolName != "test_tool" {
		t.Errorf("Expected tool name 'test_tool', got '%s'", capturedResult.ToolName)
	}

	if capturedResult.Error != nil {
		t.Errorf("Expected no error, got %v", capturedResult.Error)
	}

	if capturedResult.Result != "success result" {
		t.Errorf("Expected result 'success result', got %v", capturedResult.Result)
	}
}

// Test OnToolError callback
func TestOnToolError(t *testing.T) {
	called := false
	var capturedToolCall types.ToolCall
	var capturedError error

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("tool execution failed")
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: "call_1", ToolName: "test_tool", Arguments: map[string]interface{}{}},
				},
				Usage: types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Handled error",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnToolError: func(toolCall types.ToolCall, err error) {
			called = true
			capturedToolCall = toolCall
			capturedError = err
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnToolError was not called")
	}

	if capturedToolCall.ToolName != "test_tool" {
		t.Errorf("Expected tool name 'test_tool', got '%s'", capturedToolCall.ToolName)
	}

	if capturedError == nil {
		t.Error("Expected error to be captured")
	}

	if capturedError.Error() != "tool execution failed" {
		t.Errorf("Expected error 'tool execution failed', got '%s'", capturedError.Error())
	}
}

// Test OnToolError with tool not found
func TestOnToolError_ToolNotFound(t *testing.T) {
	called := false
	var capturedError error

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: "call_1", ToolName: "nonexistent_tool", Arguments: map[string]interface{}{}},
				},
				Usage: types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Done",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		OnToolError: func(toolCall types.ToolCall, err error) {
			called = true
			capturedError = err
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnToolError was not called for missing tool")
	}

	if capturedError == nil {
		t.Fatal("Expected error to be captured")
	}

	if capturedError.Error() != "tool not found: nonexistent_tool" {
		t.Errorf("Expected 'tool not found' error, got '%s'", capturedError.Error())
	}
}

// Test all LangChain callbacks together
func TestLangChainCallbacks_AllTogether(t *testing.T) {
	chainStartCalled := false
	chainEndCalled := false
	agentActionCalled := false
	agentFinishCalled := false
	toolStartCalled := false
	toolEndCalled := false

	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Using tool",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: "call_1", ToolName: "test_tool", Arguments: map[string]interface{}{}},
				},
				Usage: types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Final answer",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		OnChainStart: func(input string, messages []types.Message) {
			chainStartCalled = true
		},
		OnChainEnd: func(result *AgentResult) {
			chainEndCalled = true
		},
		OnAgentAction: func(action AgentAction) {
			agentActionCalled = true
		},
		OnAgentFinish: func(finish AgentFinish) {
			agentFinishCalled = true
		},
		OnToolStart: func(toolCall types.ToolCall) {
			toolStartCalled = true
		},
		OnToolEnd: func(toolResult types.ToolResult) {
			toolEndCalled = true
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !chainStartCalled {
		t.Error("OnChainStart was not called")
	}
	if !chainEndCalled {
		t.Error("OnChainEnd was not called")
	}
	if !agentActionCalled {
		t.Error("OnAgentAction was not called")
	}
	if !agentFinishCalled {
		t.Error("OnAgentFinish was not called")
	}
	if !toolStartCalled {
		t.Error("OnToolStart was not called")
	}
	if !toolEndCalled {
		t.Error("OnToolEnd was not called")
	}
}
