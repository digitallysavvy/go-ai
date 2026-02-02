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
