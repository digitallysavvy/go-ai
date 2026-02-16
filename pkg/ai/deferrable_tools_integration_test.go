// +build integration

package ai

import (
	"context"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// TestIntegration_AnthropicToolSearch tests Anthropic's tool-search with error handling
// Requires ANTHROPIC_API_KEY environment variable
// Run with: go test -v -tags=integration -run TestIntegration_AnthropicToolSearch
func TestIntegration_AnthropicToolSearch(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	provider := anthropic.New(apiKey)
	model := provider.LanguageModel("claude-3-5-sonnet-20241022")

	// Define Anthropic's built-in tool-search tool
	toolSearch := types.Tool{
		Name:        "tool-search-bm25",
		Description: "Search for tools using BM25 search algorithm",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Search for weather tools using tool-search-bm25",
		Tools:  []types.Tool{toolSearch},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no crashes occurred
	if result == nil {
		t.Fatal("result should not be nil")
	}

	t.Logf("Result: %s", result.Text)
	t.Logf("Steps: %d", len(result.Steps))
	t.Logf("Tool results: %d", len(result.ToolResults))

	// Check that provider-executed tools are marked correctly
	for _, tr := range result.ToolResults {
		if tr.ToolName == "tool-search-bm25" {
			if !tr.ProviderExecuted {
				t.Error("tool-search-bm25 should be marked as provider-executed")
			}
			t.Logf("Tool result: ToolCallID=%s, HasResult=%v, HasError=%v",
				tr.ToolCallID, tr.Result != nil, tr.Error != nil)
		}
	}
}

// TestIntegration_AnthropicToolSearchError tests error handling for invalid tool search
// Requires ANTHROPIC_API_KEY environment variable
func TestIntegration_AnthropicToolSearchError(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	provider := anthropic.New(apiKey)
	model := provider.LanguageModel("claude-3-5-sonnet-20241022")

	// Define tool-search with invalid parameters to trigger error
	toolSearch := types.Tool{
		Name:        "tool-search-bm25",
		Description: "Search for tools",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type": "string",
				},
			},
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Search for xyz",
		Tools:  []types.Tool{toolSearch},
	})

	// Should not crash even if tool returns error
	if err != nil {
		t.Logf("Expected potential error: %v", err)
	}

	if result != nil {
		t.Logf("Result received despite potential error")
		t.Logf("Steps: %d", len(result.Steps))
		t.Logf("Tool results: %d", len(result.ToolResults))

		// Verify error handling
		for _, tr := range result.ToolResults {
			if tr.Error != nil {
				t.Logf("Tool error handled correctly: %v", tr.Error)
			}
		}
	}
}

// TestIntegration_AnthropicWebSearch tests Anthropic's web-search tool
// Requires ANTHROPIC_API_KEY environment variable
func TestIntegration_AnthropicWebSearch(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	provider := anthropic.New(apiKey)
	model := provider.LanguageModel("claude-3-5-sonnet-20241022")

	webSearch := types.Tool{
		Name:        "web-search",
		Description: "Search the web for information",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Search the web for latest AI news",
		Tools:  []types.Tool{webSearch},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}

	t.Logf("Result: %s", result.Text)

	// Verify provider-executed tool handling
	for _, tr := range result.ToolResults {
		if tr.ToolName == "web-search" {
			if !tr.ProviderExecuted {
				t.Error("web-search should be marked as provider-executed")
			}
		}
	}
}

// TestIntegration_MixedTools tests both local and provider-executed tools together
// Requires ANTHROPIC_API_KEY environment variable
func TestIntegration_MixedTools(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	provider := anthropic.New(apiKey)
	model := provider.LanguageModel("claude-3-5-sonnet-20241022")

	// Local tool
	localExecuted := false
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get current weather",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type": "string",
				},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			localExecuted = true
			return map[string]interface{}{
				"temperature": 72,
				"condition":   "sunny",
			}, nil
		},
	}

	// Provider-executed tool
	webSearch := types.Tool{
		Name:        "web-search",
		Description: "Search the web",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type": "string",
				},
			},
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Get the weather in San Francisco and search for weather patterns",
		Tools:  []types.Tool{weatherTool, webSearch},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !localExecuted {
		t.Error("expected local tool to be executed")
	}

	// Verify both types of tools are handled correctly
	hasLocal := false
	hasProvider := false
	for _, tr := range result.ToolResults {
		if tr.ToolName == "get_weather" {
			hasLocal = true
			if tr.ProviderExecuted {
				t.Error("local tool should not be marked as provider-executed")
			}
		}
		if tr.ToolName == "web-search" {
			hasProvider = true
			if !tr.ProviderExecuted {
				t.Error("web-search should be marked as provider-executed")
			}
		}
	}

	t.Logf("Has local tool: %v, Has provider tool: %v", hasLocal, hasProvider)
}

// TestIntegration_ToolTimeout tests timeout handling with provider-executed tools
// Requires ANTHROPIC_API_KEY environment variable
func TestIntegration_ToolTimeout(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	provider := anthropic.New(apiKey)
	model := provider.LanguageModel("claude-3-5-sonnet-20241022")

	webSearch := types.Tool{
		Name:        "web-search",
		Description: "Search the web",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type": "string",
				},
			},
		},
	}

	// Very short timeout to test error handling
	timeout := &TimeoutConfig{
		Total: intPtr(5000), // 5 seconds
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:   model,
		Prompt:  "Search for detailed information about AI",
		Tools:   []types.Tool{webSearch},
		Timeout: timeout,
	})

	// May timeout, but should not crash
	if err != nil {
		t.Logf("Expected timeout or error: %v", err)
	}
}

// intPtr returns a pointer to an int
func intPtr(i int) *int {
	return &i
}
