package main

import (
	"context"
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Println("Please set ANTHROPIC_API_KEY environment variable")
		os.Exit(1)
	}

	// Create provider and model
	provider := anthropic.New(apiKey)
	model := provider.LanguageModel("claude-3-5-sonnet-20241022")

	// Example 1: Single Provider-Executed Tool
	fmt.Println("=== Example 1: Provider-Executed Tool (tool-search-bm25) ===")
	runToolSearchExample(model)

	// Example 2: Mixed Local and Provider Tools
	fmt.Println("\n=== Example 2: Mixed Local and Provider Tools ===")
	runMixedToolsExample(model)

	// Example 3: Error Handling
	fmt.Println("\n=== Example 3: Error Handling ===")
	runErrorHandlingExample(model)
}

// runToolSearchExample demonstrates a provider-executed tool
func runToolSearchExample(model ai.LanguageModel) {
	// Define Anthropic's tool-search-bm25 tool
	toolSearch := types.Tool{
		Name:        "tool-search-bm25",
		Description: "Search for tools using BM25 search algorithm",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query for finding tools",
				},
			},
			"required": []string{"query"},
		},
		// Note: No Execute function - this tool is executed by the provider
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Search for weather-related tools using tool-search-bm25",
		Tools:  []types.Tool{toolSearch},
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Steps: %d\n", len(result.Steps))
	fmt.Printf("Tool Results: %d\n", len(result.ToolResults))

	// Inspect tool results
	for _, tr := range result.ToolResults {
		fmt.Printf("\nTool: %s\n", tr.ToolName)
		fmt.Printf("  Provider-Executed: %v\n", tr.ProviderExecuted)
		if tr.Error != nil {
			fmt.Printf("  Error: %v\n", tr.Error)
		} else {
			fmt.Printf("  Result: %v\n", tr.Result)
		}
	}
}

// runMixedToolsExample demonstrates using both local and provider-executed tools
func runMixedToolsExample(model ai.LanguageModel) {
	// Local tool: Get weather (simulated)
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "City name",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			location := input["location"].(string)
			fmt.Printf("  [LOCAL] Executing get_weather for: %s\n", location)

			// Simulate API call
			return map[string]interface{}{
				"temperature": 72,
				"condition":   "sunny",
				"location":    location,
				"humidity":    65,
			}, nil
		},
	}

	// Provider-executed tool: Web search
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
		// Note: No Execute function - provider handles this
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What's the weather in San Francisco? Also search the web for recent weather patterns in California.",
		Tools:  []types.Tool{weatherTool, webSearch},
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("\nTool Execution Summary:\n")

	// Show which tools were executed and how
	localCount := 0
	providerCount := 0
	for _, tr := range result.ToolResults {
		if tr.ProviderExecuted {
			providerCount++
			fmt.Printf("  ✓ %s (provider-executed)\n", tr.ToolName)
		} else {
			localCount++
			fmt.Printf("  ✓ %s (local)\n", tr.ToolName)
		}
	}

	fmt.Printf("\nTotal: %d local, %d provider-executed\n", localCount, providerCount)
}

// runErrorHandlingExample demonstrates error handling for provider-executed tools
func runErrorHandlingExample(model ai.LanguageModel) {
	// Define a tool that might error
	webFetch := types.Tool{
		Name:        "web-fetch",
		Description: "Fetch content from a URL",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to fetch",
				},
			},
			"required": []string{"url"},
		},
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Fetch content from https://invalid-url-that-does-not-exist-12345.com",
		Tools:  []types.Tool{webFetch},
	})

	if err != nil {
		fmt.Printf("Generation error: %v\n", err)
		// Even with an error, check if we got partial results
		if result != nil {
			fmt.Printf("Partial results available\n")
		}
		return
	}

	fmt.Printf("Response: %s\n", result.Text)

	// Check for tool errors
	fmt.Printf("\nTool Results:\n")
	for _, tr := range result.ToolResults {
		fmt.Printf("Tool: %s\n", tr.ToolName)
		if tr.Error != nil {
			fmt.Printf("  ⚠️  Error: %v\n", tr.Error)
		} else {
			fmt.Printf("  ✓ Success\n")
		}
	}
}
