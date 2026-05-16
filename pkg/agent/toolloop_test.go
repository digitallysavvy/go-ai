package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Helper function to create int64 pointers
func intPtr(i int64) *int64 {
	return &i
}

// Mock language model for testing
type mockLanguageModel struct {
	responses []types.GenerateResult
	callCount int
	options   []*provider.GenerateOptions
}

func (m *mockLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.options = append(m.options, opts)
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

func (m *mockLanguageModel) SpecificationVersion() string        { return "v3" }
func (m *mockLanguageModel) Provider() string                    { return "mock" }
func (m *mockLanguageModel) ModelID() string                     { return "mock-model" }
func (m *mockLanguageModel) SupportsTools() bool                 { return true }
func (m *mockLanguageModel) SupportsStructuredOutput() bool      { return false }
func (m *mockLanguageModel) DefaultObjectGenerationMode() string { return "" }
func (m *mockLanguageModel) SupportsImageUrls() bool             { return false }
func (m *mockLanguageModel) SupportsImageInput() bool            { return false }
func (m *mockLanguageModel) SupportsParallelToolCalls() bool     { return true }

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
	if step.StepNumber != 0 {
		t.Errorf("Expected step number 0, got %d", step.StepNumber)
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
						ID:        "call_1",
						ToolName:  "test_tool",
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
		Model:    mock,
		Tools:    []types.Tool{testTool},
		MaxSteps: 5,
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
		expectedStepNum := i
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
						ID:        "call_1",
						ToolName:  "test_tool",
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
						ID:        "call_2",
						ToolName:  "test_tool",
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
		Model:    mock,
		Tools:    []types.Tool{testTool},
		MaxSteps: 5,
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
						ID:        "call_1",
						ToolName:  "test_tool",
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
			if step.StepNumber == 0 {
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
	if capturedStep.StepNumber != 0 {
		t.Errorf("Expected step number 0, got %d", capturedStep.StepNumber)
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

func (m *mockErrorModel) SpecificationVersion() string        { return "v3" }
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

	if action.StepNumber != 0 {
		t.Errorf("Expected step number 0, got %d", action.StepNumber)
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

	if capturedFinish.StepNumber != 0 {
		t.Errorf("Expected step number 0, got %d", capturedFinish.StepNumber)
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

	if capturedFinish.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("Expected finish reason 'tool-calls', got '%s'", capturedFinish.FinishReason)
	}

	// MaxSteps is now converted to StopWhen{StepCountIs(N)}, so stop_reason should be set
	if stopReason, ok := capturedFinish.Metadata["stop_reason"].(string); !ok || stopReason == "" {
		t.Error("Expected stop_reason metadata to be set")
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

// Test run tracking - automatic RunID generation
func TestRunTracking_AutomaticRunID(t *testing.T) {
	var capturedRunID string

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
			capturedRunID = finish.RunID
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if capturedRunID == "" {
		t.Error("RunID was not automatically generated")
	}

	// Verify it's a valid UUID format (36 characters with dashes)
	if len(capturedRunID) != 36 {
		t.Errorf("RunID has invalid length: %d, expected 36", len(capturedRunID))
	}
}

// Test run tracking - custom RunID
func TestRunTracking_CustomRunID(t *testing.T) {
	customRunID := "my-custom-run-id-123"
	var capturedRunID string

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
			capturedRunID = finish.RunID
		},
	})

	ctx := WithRunID(context.Background(), customRunID)
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if capturedRunID != customRunID {
		t.Errorf("Expected custom RunID %q, got %q", customRunID, capturedRunID)
	}
}

// Test run tracking - ParentRunID and Tags
func TestRunTracking_ParentAndTags(t *testing.T) {
	parentRunID := "parent-run-123"
	tags := []string{"production", "user:456", "session:abc"}
	var capturedAction AgentAction
	var capturedFinish AgentFinish

	testTool := types.Tool{
		Name:        "test",
		Description: "Test tool",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{"type": "string"},
			},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text: "Let me use the tool",
				ToolCalls: []types.ToolCall{
					{
						ID:        "call-1",
						ToolName:  "test",
						Arguments: map[string]interface{}{"input": "test"},
					},
				},
				FinishReason: types.FinishReasonToolCalls,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
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
		OnAgentAction: func(action AgentAction) {
			capturedAction = action
		},
		OnAgentFinish: func(finish AgentFinish) {
			capturedFinish = finish
		},
	})

	ctx := context.Background()
	ctx = WithParentRunID(ctx, parentRunID)
	ctx = WithTags(ctx, tags)

	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify AgentAction has tracking info
	if capturedAction.ParentRunID != parentRunID {
		t.Errorf("AgentAction.ParentRunID = %q, expected %q", capturedAction.ParentRunID, parentRunID)
	}
	if len(capturedAction.Tags) != len(tags) {
		t.Errorf("AgentAction.Tags length = %d, expected %d", len(capturedAction.Tags), len(tags))
	}
	for i, tag := range tags {
		if i < len(capturedAction.Tags) && capturedAction.Tags[i] != tag {
			t.Errorf("AgentAction.Tags[%d] = %q, expected %q", i, capturedAction.Tags[i], tag)
		}
	}

	// Verify AgentFinish has tracking info
	if capturedFinish.ParentRunID != parentRunID {
		t.Errorf("AgentFinish.ParentRunID = %q, expected %q", capturedFinish.ParentRunID, parentRunID)
	}
	if len(capturedFinish.Tags) != len(tags) {
		t.Errorf("AgentFinish.Tags length = %d, expected %d", len(capturedFinish.Tags), len(tags))
	}
}

// Test run tracking helper functions
func TestRunTracking_Helpers(t *testing.T) {
	ctx := context.Background()

	// Test GetRunID on empty context
	if runID := GetRunID(ctx); runID != "" {
		t.Errorf("GetRunID on empty context should return empty string, got %q", runID)
	}

	// Test WithRunID and GetRunID
	testRunID := "test-run-123"
	ctx = WithRunID(ctx, testRunID)
	if runID := GetRunID(ctx); runID != testRunID {
		t.Errorf("GetRunID() = %q, expected %q", runID, testRunID)
	}

	// Test GetParentRunID on empty context
	if parentRunID := GetParentRunID(context.Background()); parentRunID != "" {
		t.Errorf("GetParentRunID on empty context should return empty string, got %q", parentRunID)
	}

	// Test WithParentRunID and GetParentRunID
	testParentRunID := "parent-run-456"
	ctx = WithParentRunID(ctx, testParentRunID)
	if parentRunID := GetParentRunID(ctx); parentRunID != testParentRunID {
		t.Errorf("GetParentRunID() = %q, expected %q", parentRunID, testParentRunID)
	}

	// Test GetTags on empty context
	if tags := GetTags(context.Background()); tags != nil {
		t.Errorf("GetTags on empty context should return nil, got %v", tags)
	}

	// Test WithTags and GetTags
	testTags := []string{"tag1", "tag2", "tag3"}
	ctx = WithTags(ctx, testTags)
	retrievedTags := GetTags(ctx)
	if len(retrievedTags) != len(testTags) {
		t.Errorf("GetTags() returned %d tags, expected %d", len(retrievedTags), len(testTags))
	}
	for i, tag := range testTags {
		if i < len(retrievedTags) && retrievedTags[i] != tag {
			t.Errorf("GetTags()[%d] = %q, expected %q", i, retrievedTags[i], tag)
		}
	}
}

// Test run tracking - RunID propagation across steps
func TestRunTracking_PropagationAcrossSteps(t *testing.T) {
	var capturedRunIDs []string

	testTool := types.Tool{
		Name:        "test",
		Description: "Test tool",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{"type": "string"},
			},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	mock := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text: "Step 1",
				ToolCalls: []types.ToolCall{
					{
						ID:        "call-1",
						ToolName:  "test",
						Arguments: map[string]interface{}{"input": "test"},
					},
				},
				FinishReason: types.FinishReasonToolCalls,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text: "Step 2",
				ToolCalls: []types.ToolCall{
					{
						ID:        "call-2",
						ToolName:  "test",
						Arguments: map[string]interface{}{"input": "test"},
					},
				},
				FinishReason: types.FinishReasonToolCalls,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
			{
				Text:         "Final answer",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(10)},
			},
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    mock,
		Tools:    []types.Tool{testTool},
		MaxSteps: 5,
		OnAgentAction: func(action AgentAction) {
			capturedRunIDs = append(capturedRunIDs, action.RunID)
		},
	})

	ctx := context.Background()
	_, err := agent.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should have 2 actions (one per step before final)
	if len(capturedRunIDs) != 2 {
		t.Fatalf("Expected 2 OnAgentAction calls, got %d", len(capturedRunIDs))
	}

	// All should have the same RunID
	firstRunID := capturedRunIDs[0]
	for i, runID := range capturedRunIDs {
		if runID != firstRunID {
			t.Errorf("RunID at index %d = %q, expected all to be %q", i, runID, firstRunID)
		}
	}
}

// --- StopWhen tests ---

func TestToolLoopAgent_StopWhen_StepCountIs(t *testing.T) {
	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	// 5 responses all requesting tool calls
	responses := make([]types.GenerateResult, 5)
	for i := range responses {
		responses[i] = types.GenerateResult{
			Text:         fmt.Sprintf("Step %d", i+1),
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: fmt.Sprintf("%d", i+1), ToolName: "test_tool", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(10)},
		}
	}

	mock := &mockLanguageModel{responses: responses}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    mock,
		Tools:    []types.Tool{testTool},
		StopWhen: []ai.StopCondition{ai.StepCountIs(3)},
	})

	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
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

func TestToolLoopAgent_StopWhen_OverridesMaxSteps(t *testing.T) {
	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	responses := make([]types.GenerateResult, 10)
	for i := range responses {
		responses[i] = types.GenerateResult{
			Text:         fmt.Sprintf("Step %d", i+1),
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: fmt.Sprintf("%d", i+1), ToolName: "test_tool", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(10)},
		}
	}

	mock := &mockLanguageModel{responses: responses}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    mock,
		Tools:    []types.Tool{testTool},
		MaxSteps: 2,
		StopWhen: []ai.StopCondition{ai.StepCountIs(5)},
	})

	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// StopWhen should take precedence: 5 steps, not 2
	if len(result.Steps) != 5 {
		t.Errorf("expected 5 steps (StopWhen wins over MaxSteps=2), got %d", len(result.Steps))
	}
}

func TestToolLoopAgent_StopWhen_OnAgentFinishMetadata(t *testing.T) {
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

	responses := make([]types.GenerateResult, 5)
	for i := range responses {
		responses[i] = types.GenerateResult{
			Text:         fmt.Sprintf("Step %d", i+1),
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: fmt.Sprintf("%d", i+1), ToolName: "test_tool", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(10)},
		}
	}

	mock := &mockLanguageModel{responses: responses}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    mock,
		Tools:    []types.Tool{testTool},
		StopWhen: []ai.StopCondition{ai.StepCountIs(3)},
		OnAgentFinish: func(finish AgentFinish) {
			called = true
			capturedFinish = finish
		},
	})

	_, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("OnAgentFinish was not called when stop condition triggered")
	}

	if stopReason, ok := capturedFinish.Metadata["stop_reason"].(string); !ok || stopReason == "" {
		t.Error("Expected stop_reason metadata to be set")
	}
}

func TestToolLoopAgent_StopWhen_HasToolCall(t *testing.T) {
	searchTool := types.Tool{
		Name:        "search",
		Description: "Search",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "search result", nil
		},
	}
	finishTool := types.Tool{
		Name:        "finish",
		Description: "Finish",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		},
	}

	responses := []types.GenerateResult{
		{
			Text:         "Searching",
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "search", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(10)},
		},
		{
			Text:         "Finishing",
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "2", ToolName: "finish", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(10)},
		},
	}

	mock := &mockLanguageModel{responses: responses}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    mock,
		Tools:    []types.Tool{searchTool, finishTool},
		StopWhen: []ai.StopCondition{ai.HasToolCall("finish")},
	})

	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
	if result.StopReason != "tool 'finish' was called" {
		t.Errorf("expected HasToolCall stop reason, got %q", result.StopReason)
	}
}

func TestToolLoopAgent_StopWhen_Default(t *testing.T) {
	testTool := types.Tool{
		Name:        "test_tool",
		Description: "Test",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}

	// TypeScript parity: neither MaxSteps nor StopWhen set defaults to isStepCount(20).
	responses := make([]types.GenerateResult, 25)
	for i := range responses {
		responses[i] = types.GenerateResult{
			Text:         fmt.Sprintf("Step %d", i+1),
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: fmt.Sprintf("%d", i+1), ToolName: "test_tool", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(10)},
		}
	}

	mock := &mockLanguageModel{responses: responses}

	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
	})

	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(result.Steps) != 20 {
		t.Errorf("expected 20 steps (default), got %d", len(result.Steps))
	}
	if result.StopReason != "maximum number of steps (20) reached" {
		t.Errorf("expected default stop reason, got %q", result.StopReason)
	}
}

func TestToolLoopAgent_RuntimeAndToolsContext(t *testing.T) {
	toolCtx := map[string]interface{}{"tenant": "acme"}
	runtimeCtx := map[string]interface{}{"requestID": "req-1"}
	var gotOptions types.ToolExecutionOptions

	testTool := types.Tool{
		Name:          "ctx_tool",
		Description:   "uses context",
		Parameters:    map[string]interface{}{"type": "object"},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"tenant"}}),
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			gotOptions = opts
			return "ok", nil
		},
	}

	mock := &mockLanguageModel{responses: []types.GenerateResult{
		{
			Text:         "call",
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "ctx_tool", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(1)},
		},
		{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}},
	}}
	agent := NewToolLoopAgent(AgentConfig{
		Model:          mock,
		Tools:          []types.Tool{testTool},
		RuntimeContext: runtimeCtx,
		ToolsContext:   map[string]interface{}{"ctx_tool": toolCtx},
		MaxSteps:       3,
	})

	if _, err := agent.Execute(context.Background(), "test"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !reflect.DeepEqual(gotOptions.RuntimeContext, runtimeCtx) {
		t.Fatalf("RuntimeContext not forwarded")
	}
	if !reflect.DeepEqual(gotOptions.ToolContext, toolCtx) {
		t.Fatalf("ToolContext not forwarded")
	}
}

func TestToolLoopAgent_InvalidToolContextReturnsValidationResult(t *testing.T) {
	testTool := types.Tool{
		Name:          "ctx_tool",
		Description:   "uses context",
		Parameters:    map[string]interface{}{"type": "object"},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"tenant"}}),
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("tool should not execute with invalid context")
			return nil, nil
		},
	}
	mock := &mockLanguageModel{responses: []types.GenerateResult{
		{
			Text:         "call",
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "ctx_tool", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(1)},
		},
		{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}},
	}}
	agent := NewToolLoopAgent(AgentConfig{
		Model:        mock,
		Tools:        []types.Tool{testTool},
		ToolsContext: map[string]interface{}{"ctx_tool": map[string]interface{}{}},
		MaxSteps:     2,
	})

	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result.ToolResults) == 0 || result.ToolResults[0].Error == nil {
		t.Fatalf("expected validation error result")
	}
	if !strings.Contains(result.ToolResults[0].Error.Error(), "toolsContext.ctx_tool") {
		t.Fatalf("expected schema path in error, got %v", result.ToolResults[0].Error)
	}
}

func TestToolLoopAgent_CallOptionsSchemaBeforeModel(t *testing.T) {
	mock := &mockLanguageModel{}
	agent := NewToolLoopAgent(AgentConfig{
		Model:             mock,
		CallOptions:       map[string]interface{}{"mode": "bad"},
		CallOptionsSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"ok"}}),
	})

	_, err := agent.Execute(context.Background(), "test")
	if err == nil {
		t.Fatal("expected call options validation error")
	}
	if mock.callCount != 0 {
		t.Fatal("model was called before call options validation")
	}
	if !strings.Contains(err.Error(), "options") {
		t.Fatalf("expected options path in error, got %v", err)
	}
}

func TestToolLoopAgent_ApprovalNilAndDenied(t *testing.T) {
	called := false
	testTool := types.Tool{
		Name:        "delete",
		Description: "delete",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			called = true
			return "deleted", nil
		},
	}
	mock := &mockLanguageModel{responses: []types.GenerateResult{
		{Text: "call", FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "delete", Arguments: map[string]interface{}{}}}, Usage: types.Usage{TotalTokens: intPtr(1)}},
		{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}},
	}}
	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{testTool},
		ToolApproval: types.ToolApprovalFunc(func(types.ToolCall, []types.Tool, []types.Message, interface{}, map[string]interface{}) types.ToolApprovalResult {
			return types.ToolApprovalResult{}
		}),
		MaxSteps: 2,
	})
	if _, err := agent.Execute(context.Background(), "test"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !called {
		t.Fatal("zero-value approval should be not-applicable and execute")
	}

	mock = &mockLanguageModel{responses: []types.GenerateResult{
		{Text: "call", FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "delete", Arguments: map[string]interface{}{}}}, Usage: types.Usage{TotalTokens: intPtr(1)}},
	}}
	reason := "policy"
	agent = NewToolLoopAgent(AgentConfig{
		Model:        mock,
		Tools:        []types.Tool{testTool},
		ToolApproval: map[string]types.ToolApprovalValue{"delete": types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}},
		MaxSteps:     1,
	})
	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.ToolResults[0].ApprovalStatus != types.ToolApprovalStatusDenied || *result.ToolResults[0].ApprovalReason != "policy" {
		t.Fatalf("denial not preserved: %+v", result.ToolResults[0])
	}
}

func TestToolLoopAgent_FilterActiveToolsAndPrompt(t *testing.T) {
	mock := &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}}}}
	agent := NewToolLoopAgent(AgentConfig{
		Model:  mock,
		Prompt: "system from prompt",
		Tools: []types.Tool{
			{Name: "keep", Description: "keep", Parameters: map[string]interface{}{"type": "object"}},
			{Name: "drop", Description: "drop", Parameters: map[string]interface{}{"type": "object"}},
		},
		FilterActiveTools: func(ctx context.Context, stepNumber int, tools []types.Tool) []types.Tool {
			return []types.Tool{tools[0]}
		},
	})

	if _, err := agent.Execute(context.Background(), "test"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := mock.options[0].Prompt.System; got != "system from prompt" {
		t.Fatalf("Prompt alias not used as system prompt: %q", got)
	}
	if len(mock.options[0].Tools) != 1 || mock.options[0].Tools[0].Name != "keep" {
		t.Fatalf("FilterActiveTools not applied: %+v", mock.options[0].Tools)
	}
}

func TestToolLoopAgent_InvalidToolCallPreserved(t *testing.T) {
	invalidErr := errors.New("Invalid input for tool test_tool")
	testTool := types.Tool{Name: "test_tool", Description: "test", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		t.Fatal("invalid tool call should not execute")
		return nil, nil
	}}
	mock := &mockLanguageModel{responses: []types.GenerateResult{{
		Text:         "bad call",
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "bad-1", ToolName: "test_tool", Invalid: true, Error: invalidErr}},
		Usage:        types.Usage{TotalTokens: intPtr(1)},
	}}}
	agent := NewToolLoopAgent(AgentConfig{Model: mock, Tools: []types.Tool{testTool}, MaxSteps: 1})
	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result.ToolResults) != 1 || !errors.Is(result.ToolResults[0].Error, invalidErr) {
		t.Fatalf("invalid tool call error not preserved: %+v", result.ToolResults)
	}
}

func TestToolLoopAgent_GeneratePerCallOptionsOverrideConfig(t *testing.T) {
	baseTemp := 0.1
	callTemp := 0.7
	maxTokens := 42
	seed := 9
	runtimeCtx := map[string]interface{}{"requestID": "call"}
	providerOpts := map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "low"}}
	mock := &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}}}}
	agent := NewToolLoopAgent(AgentConfig{Model: mock, Temperature: &baseTemp})

	started := false
	result, err := agent.Generate(context.Background(), AgentGenerateOptions{
		Prompt:          "hello",
		System:          "per-call system",
		Temperature:     &callTemp,
		MaxTokens:       &maxTokens,
		Seed:            &seed,
		RuntimeContext:  runtimeCtx,
		ToolsContext:    map[string]interface{}{"tool": map[string]interface{}{"tenant": "acme"}},
		ToolChoice:      types.RequiredToolChoice(),
		ProviderOptions: providerOpts,
		OnStart: func(ctx context.Context, e ai.OnStartEvent) {
			started = true
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("unexpected result text: %q", result.Text)
	}
	if !started {
		t.Fatal("per-call OnStart callback was not invoked")
	}
	got := mock.options[0]
	if got.Prompt.System != "per-call system" {
		t.Fatalf("system override not forwarded: %q", got.Prompt.System)
	}
	if got.Temperature == nil || *got.Temperature != callTemp {
		t.Fatalf("temperature override not forwarded: %v", got.Temperature)
	}
	if got.MaxTokens == nil || *got.MaxTokens != maxTokens {
		t.Fatalf("max tokens override not forwarded: %v", got.MaxTokens)
	}
	if got.Seed == nil || *got.Seed != seed {
		t.Fatalf("seed override not forwarded: %v", got.Seed)
	}
	if got.ToolChoice.Type != types.ToolChoiceRequired {
		t.Fatalf("tool choice override not forwarded: %+v", got.ToolChoice)
	}
	if !reflect.DeepEqual(got.RuntimeContext, runtimeCtx) {
		t.Fatalf("runtime context override not forwarded: %+v", got.RuntimeContext)
	}
	if !reflect.DeepEqual(got.ProviderOptions, providerOpts) {
		t.Fatalf("provider options override not forwarded: %+v", got.ProviderOptions)
	}
}

func TestToolLoopAgent_OnStepStartReceivesFilteredTools(t *testing.T) {
	mock := &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}}}}
	var eventTools []types.Tool
	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{
			{Name: "keep", Parameters: map[string]interface{}{"type": "object"}},
			{Name: "drop", Parameters: map[string]interface{}{"type": "object"}},
		},
		FilterActiveTools: func(ctx context.Context, stepNumber int, tools []types.Tool) []types.Tool {
			return []types.Tool{tools[0]}
		},
		OnStepStartEvent: func(ctx context.Context, e ai.OnStepStartEvent) {
			eventTools = e.Tools
		},
	})
	if _, err := agent.Execute(context.Background(), "test"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(eventTools) != 1 || eventTools[0].Name != "keep" {
		t.Fatalf("OnStepStart tools were not filtered: %+v", eventTools)
	}
}

func TestToolLoopAgent_FilteredToolsUsedForExecution(t *testing.T) {
	blockedExecuted := false
	blocked := types.Tool{Name: "blocked", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		blockedExecuted = true
		return "blocked", nil
	}}
	allowed := types.Tool{Name: "allowed", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "allowed", nil
	}}
	mock := &mockLanguageModel{responses: []types.GenerateResult{{
		Text:         "call",
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "blocked", Arguments: map[string]interface{}{"x": 1}}},
		Usage:        types.Usage{TotalTokens: intPtr(1)},
	}}}
	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{allowed, blocked},
		FilterActiveTools: func(ctx context.Context, stepNumber int, tools []types.Tool) []types.Tool {
			return []types.Tool{allowed}
		},
	})
	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if blockedExecuted {
		t.Fatal("filtered-out tool executed")
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Error == nil || !strings.Contains(result.ToolResults[0].Error.Error(), "tool not found") {
		t.Fatalf("expected not-found result for inactive tool, got %+v", result.ToolResults)
	}
}

func TestToolLoopAgent_ResponseMessagesPreserveAssistantToolCalls(t *testing.T) {
	tool := types.Tool{Name: "lookup", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	mock := &mockLanguageModel{responses: []types.GenerateResult{
		{Text: "calling", FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"q": "x"}}}, Usage: types.Usage{TotalTokens: intPtr(1)}},
		{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}},
	}}
	agent := NewToolLoopAgent(AgentConfig{Model: mock, Tools: []types.Tool{tool}, MaxSteps: 2})
	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result.Steps[0].ResponseMessages) == 0 || len(result.Steps[0].ResponseMessages[0].ToolCalls) != 1 {
		t.Fatalf("step response messages did not preserve tool calls: %+v", result.Steps[0].ResponseMessages)
	}
	secondCallMessages := mock.options[1].Prompt.Messages
	if len(secondCallMessages) < 2 || len(secondCallMessages[1].ToolCalls) != 1 {
		t.Fatalf("follow-up prompt did not preserve assistant tool calls: %+v", secondCallMessages)
	}
}

func TestToolLoopAgent_NilExecuteToolDoesNotPanicOrContinue(t *testing.T) {
	mock := &mockLanguageModel{responses: []types.GenerateResult{{
		Text:         "call client tool",
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "client_tool", Arguments: map[string]interface{}{}}},
		Usage:        types.Usage{TotalTokens: intPtr(1)},
	}}}
	agent := NewToolLoopAgent(AgentConfig{Model: mock, Tools: []types.Tool{{Name: "client_tool", Parameters: map[string]interface{}{"type": "object"}}}})
	result, err := agent.Execute(context.Background(), "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if mock.callCount != 1 {
		t.Fatalf("expected one model call, got %d", mock.callCount)
	}
	if len(result.ToolResults) != 0 {
		t.Fatalf("nil Execute tool should not synthesize local results: %+v", result.ToolResults)
	}
}

func TestToolLoopAgent_ToolApprovalReceivesActiveToolsAndMessages(t *testing.T) {
	tool := types.Tool{Name: "allowed", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	mock := &mockLanguageModel{responses: []types.GenerateResult{
		{Text: "call", FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "allowed", Arguments: map[string]interface{}{}}}, Usage: types.Usage{TotalTokens: intPtr(1)}},
		{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}},
	}}
	var gotToolNames []string
	gotMessages := 0
	agent := NewToolLoopAgent(AgentConfig{
		Model: mock,
		Tools: []types.Tool{tool, types.Tool{Name: "inactive", Parameters: map[string]interface{}{"type": "object"}}},
		FilterActiveTools: func(ctx context.Context, stepNumber int, tools []types.Tool) []types.Tool {
			return []types.Tool{tool}
		},
		ToolApproval: types.ToolApprovalFunc(func(call types.ToolCall, tools []types.Tool, messages []types.Message, runtimeCtx interface{}, toolsCtx map[string]interface{}) types.ToolApprovalResult {
			for _, tool := range tools {
				gotToolNames = append(gotToolNames, tool.Name)
			}
			gotMessages = len(messages)
			return types.ToolApprovalResult{}
		}),
		MaxSteps: 2,
	})
	if _, err := agent.Execute(context.Background(), "test"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !reflect.DeepEqual(gotToolNames, []string{"allowed"}) {
		t.Fatalf("approval did not receive active tools: %+v", gotToolNames)
	}
	if gotMessages == 0 {
		t.Fatal("approval did not receive current messages")
	}
}

func TestToolLoopAgent_PrepareCallContextForwardedToApprovalAndExecution(t *testing.T) {
	initialCtx := map[string]interface{}{"tenant": "initial"}
	preparedCtx := map[string]interface{}{"tenant": "prepared"}
	runtimeCtx := map[string]interface{}{"requestID": "prepared"}
	var approvalToolsCtx map[string]interface{}
	var approvalRuntime interface{}
	var execRuntime interface{}
	var execToolCtx interface{}

	tool := types.Tool{
		Name:          "ctx_tool",
		Parameters:    map[string]interface{}{"type": "object"},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"tenant"}}),
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			execRuntime = opts.RuntimeContext
			execToolCtx = opts.ToolContext
			return "ok", nil
		},
	}
	mock := &mockLanguageModel{responses: []types.GenerateResult{
		{Text: "call", FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "ctx_tool", Arguments: map[string]interface{}{}}}, Usage: types.Usage{TotalTokens: intPtr(1)}},
		{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}},
	}}
	agent := NewToolLoopAgent(AgentConfig{
		Model:        mock,
		Tools:        []types.Tool{tool},
		ToolsContext: map[string]interface{}{"ctx_tool": initialCtx},
		PrepareCall: func(ctx context.Context, config PrepareCallConfig) PrepareCallConfig {
			config.RuntimeContext = runtimeCtx
			config.ToolsContext = map[string]interface{}{"ctx_tool": preparedCtx}
			return config
		},
		ToolApproval: types.ToolApprovalFunc(func(call types.ToolCall, tools []types.Tool, messages []types.Message, runtimeCtx interface{}, toolsCtx map[string]interface{}) types.ToolApprovalResult {
			approvalRuntime = runtimeCtx
			approvalToolsCtx = toolsCtx
			return types.ToolApprovalResult{}
		}),
		MaxSteps: 2,
	})
	if _, err := agent.Execute(context.Background(), "test"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !reflect.DeepEqual(approvalRuntime, runtimeCtx) {
		t.Fatalf("approval runtime context not from PrepareCall: %+v", approvalRuntime)
	}
	if !reflect.DeepEqual(approvalToolsCtx["ctx_tool"], preparedCtx) {
		t.Fatalf("approval tools context not from PrepareCall: %+v", approvalToolsCtx)
	}
	if !reflect.DeepEqual(execRuntime, runtimeCtx) {
		t.Fatalf("execution runtime context not from PrepareCall: %+v", execRuntime)
	}
	if !reflect.DeepEqual(execToolCtx, preparedCtx) {
		t.Fatalf("execution tool context not from PrepareCall: %+v", execToolCtx)
	}
}

func TestToolLoopAgent_GeneratePromptMessagesValidation(t *testing.T) {
	mock := &mockLanguageModel{}
	agent := NewToolLoopAgent(AgentConfig{Model: mock})

	if _, err := agent.Generate(context.Background(), AgentGenerateOptions{}); err == nil {
		t.Fatal("expected error when neither prompt nor messages are provided")
	}
	if _, err := agent.Generate(context.Background(), AgentGenerateOptions{
		Prompt:   "test",
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}},
	}); err == nil {
		t.Fatal("expected error when both prompt and messages are provided")
	}
}

func TestToolLoopAgentGenerateReturnsCoreGenerateTextResult(t *testing.T) {
	model := &functionalAgentLanguageModel{
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{TotalTokens: intPtr(3)},
			}, nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{Model: model})
	result, err := agent.Generate(context.Background(), AgentGenerateOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.FinalStep.Text != "ok" || result.TotalUsage.TotalTokens == nil || *result.TotalUsage.TotalTokens != 3 {
		t.Fatalf("result = %#v, want core GenerateTextResult final step and total usage", result)
	}
}

func TestToolLoopAgentPrepareCallCanOverrideModelAndInclude(t *testing.T) {
	baseModel := &functionalAgentLanguageModel{
		modelID: "base",
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			t.Fatal("base model should not be called after PrepareCall override")
			return nil, nil
		},
	}
	overrideModel := &functionalAgentLanguageModel{
		modelID: "override",
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "override",
				FinishReason: types.FinishReasonStop,
				RawRequest:   map[string]interface{}{"body": true},
			}, nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model: baseModel,
		PrepareCall: func(ctx context.Context, config PrepareCallConfig) PrepareCallConfig {
			config.Model = overrideModel
			config.Include = &ai.IncludeOptions{RequestBody: true}
			return config
		},
	})
	result, err := agent.Generate(context.Background(), AgentGenerateOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.FinalStep.Model.ModelID != "override" {
		t.Fatalf("final step model = %#v, want override", result.FinalStep.Model)
	}
	if result.Request.Body == nil {
		t.Fatal("PrepareCall include override did not retain request body")
	}
}

func TestToolLoopAgentGenerateForwardsHeadersTelemetryAndInternalFromPrepareCall(t *testing.T) {
	var generateOpts *provider.GenerateOptions
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			generateOpts = opts
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	telemetrySettings := &ai.TelemetrySettings{}
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		PrepareCall: func(ctx context.Context, config PrepareCallConfig) PrepareCallConfig {
			config.Headers = map[string]string{"x-agent": "generate"}
			config.ExperimentalTelemetry = telemetrySettings
			config.Internal = &ai.InternalOptions{
				GenerateID:     func() string { return "agent-response-id" },
				GenerateCallID: func() string { return "agent-call-id" },
			}
			return config
		},
	})
	var startCallID string
	result, err := agent.Generate(context.Background(), AgentGenerateOptions{
		Prompt: "test",
		OnStart: func(ctx context.Context, e ai.OnStartEvent) {
			startCallID = e.CallID
			if e.Headers["x-agent"] != "generate" {
				t.Fatalf("start headers = %#v", e.Headers)
			}
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if generateOpts == nil {
		t.Fatal("model was not called")
	}
	if generateOpts.Headers["x-agent"] != "generate" {
		t.Fatalf("model headers = %#v", generateOpts.Headers)
	}
	if startCallID != "agent-call-id" {
		t.Fatalf("start call id = %q, want agent-call-id", startCallID)
	}
	if result.Response.ID != "agent-response-id" {
		t.Fatalf("response id = %q, want agent-response-id", result.Response.ID)
	}
}

func TestToolLoopAgentStreamForwardsHeadersAndInternalFromPrepareCall(t *testing.T) {
	var gotHeaders map[string]string
	model := &functionalAgentLanguageModel{
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "unused", FinishReason: types.FinishReasonStop}, nil
		},
		doStream: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			gotHeaders = opts.Headers
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		PrepareCall: func(ctx context.Context, config PrepareCallConfig) PrepareCallConfig {
			config.Headers = map[string]string{"x-agent": "stream"}
			config.Internal = &ai.InternalOptions{
				GenerateCallID: func() string { return "agent-stream-call-id" },
			}
			return config
		},
	})
	var startCallID string
	result, err := agent.Stream(context.Background(), AgentStreamOptions{
		AgentGenerateOptions: AgentGenerateOptions{
			Prompt: "test",
			OnStart: func(ctx context.Context, e ai.OnStartEvent) {
				startCallID = e.CallID
				if e.Headers["x-agent"] != "stream" {
					t.Fatalf("start headers = %#v", e.Headers)
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	_ = result.Steps()
	if gotHeaders["x-agent"] != "stream" {
		t.Fatalf("model headers = %#v", gotHeaders)
	}
	if startCallID != "agent-stream-call-id" {
		t.Fatalf("start call id = %q, want agent-stream-call-id", startCallID)
	}
}

type functionalAgentLanguageModel struct {
	doGenerate func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error)
	doStream   func(context.Context, *provider.GenerateOptions) (provider.TextStream, error)
	modelID    string
}

func (m *functionalAgentLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	return m.doGenerate(ctx, opts)
}

func (m *functionalAgentLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if m.doStream != nil {
		return m.doStream(ctx, opts)
	}
	return nil, fmt.Errorf("streaming not implemented in functionalAgentLanguageModel")
}

func (m *functionalAgentLanguageModel) SpecificationVersion() string { return "v3" }
func (m *functionalAgentLanguageModel) Provider() string             { return "mock" }
func (m *functionalAgentLanguageModel) ModelID() string {
	if m.modelID != "" {
		return m.modelID
	}
	return "mock-model"
}
func (m *functionalAgentLanguageModel) SupportsTools() bool            { return true }
func (m *functionalAgentLanguageModel) SupportsStructuredOutput() bool { return false }
func (m *functionalAgentLanguageModel) SupportsImageInput() bool       { return false }

func TestToolLoopAgentExperimentalDownloadFunctionDownloadsBeforeNextStep(t *testing.T) {
	var call int
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			call++
			if call == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "tc", ToolName: "file", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			var fileBlock types.FileContentBlock
			for _, message := range opts.Prompt.Messages {
				if message.Role != types.RoleTool {
					continue
				}
				for _, part := range message.Content {
					toolResult, ok := part.(types.ToolResultContent)
					if !ok || toolResult.Output == nil {
						continue
					}
					for _, block := range toolResult.Output.Content {
						if file, ok := block.(types.FileContentBlock); ok {
							fileBlock = file
						}
					}
				}
			}
			if string(fileBlock.Data) != "agent-downloaded" || fileBlock.URL != "" || fileBlock.FileData.Type != types.FileDataTypeData {
				t.Fatalf("agent tool result file was not downloaded before next step: %+v", fileBlock)
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{
			Name: "file",
			Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
				return types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{types.FileContentBlock{
						FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.test/agent.txt", MediaType: "text/plain"},
						MediaType: "text/plain",
					}},
				}, nil
			},
		}},
		StopWhen: []ai.StopCondition{ai.StepCountIs(2)},
		ExperimentalDownload: func(ctx context.Context, requests []ai.DownloadRequest) ([]*ai.DownloadResult, error) {
			if len(requests) != 1 || requests[0].URL != "https://example.test/agent.txt" {
				t.Fatalf("download requests = %#v", requests)
			}
			return []*ai.DownloadResult{{Data: []byte("agent-downloaded")}}, nil
		},
	})
	if _, err := agent.Execute(context.Background(), "start"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestToolLoopAgentPreservesToolMetadataOnResults(t *testing.T) {
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls: []types.ToolCall{{
					ID:           "tc",
					ToolName:     "lookup",
					Arguments:    map[string]interface{}{"q": "x"},
					ToolMetadata: map[string]interface{}{"trace": "agent"},
				}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{
			Name: "lookup",
			Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
				if options.ToolMetadata["trace"] != "agent" {
					t.Fatalf("execution metadata = %+v, want agent", options.ToolMetadata)
				}
				return "ok", nil
			},
		}},
		StopWhen: []ai.StopCondition{ai.StepCountIs(1)},
	})
	result, err := agent.Execute(context.Background(), "start")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].ToolMetadata["trace"] != "agent" {
		t.Fatalf("tool result metadata = %+v, want agent", result.ToolResults)
	}
}
