package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestDeferrableTools_ProviderExecutedTool tests that provider-executed tools are handled correctly
func TestDeferrableTools_ProviderExecutedTool(t *testing.T) {
	t.Parallel()

	// Create a provider-executed tool (Anthropic tool-search)
	searchTool := types.Tool{
		Name:        "tool-search-bm25",
		Description: "Search using BM25",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("provider-executed tool should not be executed locally")
			return nil, nil
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				// First call: model requests tool
				return &types.GenerateResult{
					Text:         "",
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_1",
							ToolName:  "tool-search-bm25",
							Arguments: map[string]interface{}{"query": "test"},
						},
					},
				}, nil
			}
			// Second call: model provides final response
			return &types.GenerateResult{
				Text:         "Found results",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Search for test",
		Tools:    []types.Tool{searchTool},
		StopWhen: []StopCondition{StepCountIs(5)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Found results" {
		t.Errorf("unexpected text: %s", result.Text)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
	// Check that tool result is marked as provider-executed
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.ToolResults))
	}
	if !result.ToolResults[0].ProviderExecuted {
		t.Error("expected tool result to be marked as provider-executed")
	}
}

// TestDeferrableTools_LocalTool tests that local tools are executed normally
func TestDeferrableTools_LocalTool(t *testing.T) {
	t.Parallel()

	executed := false
	localTool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather info",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return map[string]interface{}{"temperature": 72}, nil
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					Text:         "",
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_1",
							ToolName:  "get_weather",
							Arguments: map[string]interface{}{"location": "SF"},
						},
					},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "It's 72 degrees",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "What's the weather?",
		Tools:    []types.Tool{localTool},
		StopWhen: []StopCondition{StepCountIs(5)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected local tool to be executed")
	}
	if result.ToolResults[0].ProviderExecuted {
		t.Error("expected tool result to be marked as local (not provider-executed)")
	}
}

// TestDeferrableTools_ErrorResult tests error handling for provider-executed tools
func TestDeferrableTools_ErrorResult(t *testing.T) {
	t.Parallel()

	searchTool := types.Tool{
		Name:        "tool-search-bm25",
		Description: "Search using BM25",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("provider-executed tool should not be executed locally")
			return nil, nil
		},
	}

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_1",
						ToolName:  "tool-search-bm25",
						Arguments: map[string]interface{}{"query": "test"},
					},
				},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Search for test",
		Tools:    []types.Tool{searchTool},
		MaxSteps: intPtr(1), // Only one step, so validation should catch missing result
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tool result should be present but marked as provider-executed
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.ToolResults))
	}
	if !result.ToolResults[0].ProviderExecuted {
		t.Error("expected tool result to be marked as provider-executed")
	}
}

// TestDeferrableTools_PendingResult tests handling of pending provider-executed tool results
func TestValidateToolResults_PendingResult(t *testing.T) {
	t.Parallel()

	// Pending provider-executed tools (no result or error yet) should be allowed
	// They will be resolved in subsequent provider responses
	results := []types.ToolResult{
		{
			ToolCallID:       "call_1",
			ToolName:         "tool-search-bm25",
			Result:           nil,
			Error:            nil,
			ProviderExecuted: true,
		},
	}

	err := validateToolResults(results)
	if err != nil {
		t.Fatalf("unexpected error for pending tool result: %v", err)
	}
}

// TestValidateToolResults_ValidResults tests that valid results pass validation
func TestValidateToolResults_ValidResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []types.ToolResult
		wantErr bool
	}{
		{
			name: "provider-executed with result",
			results: []types.ToolResult{
				{
					ToolCallID:       "call_1",
					ToolName:         "tool-search-bm25",
					Result:           map[string]interface{}{"results": []string{"item1"}},
					Error:            nil,
					ProviderExecuted: true,
				},
			},
			wantErr: false,
		},
		{
			name: "provider-executed with error",
			results: []types.ToolResult{
				{
					ToolCallID:       "call_1",
					ToolName:         "tool-search-bm25",
					Result:           nil,
					Error:            errors.New("search failed"),
					ProviderExecuted: true,
				},
			},
			wantErr: false,
		},
		{
			name: "local tool with result",
			results: []types.ToolResult{
				{
					ToolCallID:       "call_1",
					ToolName:         "get_weather",
					Result:           map[string]interface{}{"temp": 72},
					Error:            nil,
					ProviderExecuted: false,
				},
			},
			wantErr: false,
		},
		{
			name: "local tool with error",
			results: []types.ToolResult{
				{
					ToolCallID:       "call_1",
					ToolName:         "get_weather",
					Result:           nil,
					Error:            errors.New("weather API down"),
					ProviderExecuted: false,
				},
			},
			wantErr: false,
		},
		{
			name: "provider-executed without result or error (pending)",
			results: []types.ToolResult{
				{
					ToolCallID:       "call_1",
					ToolName:         "tool-search-bm25",
					Result:           nil,
					Error:            nil,
					ProviderExecuted: true,
				},
			},
			wantErr: false, // Pending is allowed - will be resolved in next step
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateToolResults(tt.results)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateToolResults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestIsProviderExecutedTool tests the provider tool detection
func TestIsProviderExecutedTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		want     bool
	}{
		{"Anthropic tool-search-bm25", "tool-search-bm25", true},
		{"Anthropic tool-search-regex", "tool-search-regex", true},
		{"Anthropic web-search", "web-search", true},
		{"Anthropic web-fetch", "web-fetch", true},
		{"Anthropic code-execution", "code-execution", true},
		{"xAI file-search", "file-search", true},
		{"xAI mcp-server", "mcp-server", true},
		{"local tool", "get_weather", false},
		{"local tool 2", "calculate", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &types.Tool{Name: tt.toolName}
			if got := isProviderExecutedTool(tool); got != tt.want {
				t.Errorf("isProviderExecutedTool() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDeferrableTools_MixedLocalAndProvider tests handling of both local and provider tools
func TestDeferrableTools_MixedLocalAndProvider(t *testing.T) {
	t.Parallel()

	localExecuted := false
	localTool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather info",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			localExecuted = true
			return map[string]interface{}{"temperature": 72}, nil
		},
	}

	providerTool := types.Tool{
		Name:        "tool-search-bm25",
		Description: "Search using BM25",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("provider-executed tool should not be executed locally")
			return nil, nil
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				// Request both tools
				return &types.GenerateResult{
					Text:         "",
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_1",
							ToolName:  "get_weather",
							Arguments: map[string]interface{}{"location": "SF"},
						},
						{
							ID:        "call_2",
							ToolName:  "tool-search-bm25",
							Arguments: map[string]interface{}{"query": "test"},
						},
					},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "Weather is 72 degrees, found search results",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Get weather and search",
		Tools:    []types.Tool{localTool, providerTool},
		StopWhen: []StopCondition{StepCountIs(5)},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !localExecuted {
		t.Error("expected local tool to be executed")
	}
	if len(result.ToolResults) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(result.ToolResults))
	}

	// Check that results are marked correctly
	var localResult, providerResult *types.ToolResult
	for i := range result.ToolResults {
		if result.ToolResults[i].ToolName == "get_weather" {
			localResult = &result.ToolResults[i]
		} else if result.ToolResults[i].ToolName == "tool-search-bm25" {
			providerResult = &result.ToolResults[i]
		}
	}

	if localResult == nil {
		t.Fatal("local tool result not found")
	}
	if providerResult == nil {
		t.Fatal("provider tool result not found")
	}

	if localResult.ProviderExecuted {
		t.Error("local tool should not be marked as provider-executed")
	}
	if !providerResult.ProviderExecuted {
		t.Error("provider tool should be marked as provider-executed")
	}
}

// TestToolExecutionError tests the ToolExecutionError wrapper
func TestToolExecutionError(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("network timeout")

	tests := []struct {
		name             string
		toolCallID       string
		toolName         string
		err              error
		providerExecuted bool
		wantContains     string
	}{
		{
			name:             "local tool error",
			toolCallID:       "call_1",
			toolName:         "get_weather",
			err:              baseErr,
			providerExecuted: false,
			wantContains:     "local",
		},
		{
			name:             "provider tool error",
			toolCallID:       "call_2",
			toolName:         "tool-search-bm25",
			err:              baseErr,
			providerExecuted: true,
			wantContains:     "provider-executed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wrapToolExecutionError(tt.toolCallID, tt.toolName, tt.err, tt.providerExecuted)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			toolErr, ok := err.(*types.ToolExecutionError)
			if !ok {
				t.Fatalf("expected ToolExecutionError, got %T", err)
			}

			if toolErr.ToolCallID != tt.toolCallID {
				t.Errorf("expected ToolCallID %s, got %s", tt.toolCallID, toolErr.ToolCallID)
			}
			if toolErr.ToolName != tt.toolName {
				t.Errorf("expected ToolName %s, got %s", tt.toolName, toolErr.ToolName)
			}
			if toolErr.ProviderExecuted != tt.providerExecuted {
				t.Errorf("expected ProviderExecuted %v, got %v", tt.providerExecuted, toolErr.ProviderExecuted)
			}

			errMsg := err.Error()
			if errMsg == "" {
				t.Error("error message should not be empty")
			}
		})
	}
}
