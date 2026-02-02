package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	// Create Anthropic provider
	provider := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	// Get language model
	model, err := provider.LanguageModel("claude-opus-4-20250514")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	fmt.Println("=== Example 1: BM25 Tool Search (Natural Language) ===")
	runBM25Example(model)

	fmt.Println("\n=== Example 2: Regex Tool Search (Pattern Matching) ===")
	runRegexExample(model)
}

func runBM25Example(model ai.LanguageModel) {
	// Create tool search with BM25 (natural language)
	toolSearchBM25 := tools.ToolSearchBm2520251119()

	// Create a large catalog of tools that will be deferred
	deferredTools := createLargeToolCatalog()

	// Combine tool search with deferred tools
	allTools := map[string]ai.Tool{
		"toolSearch": toolSearchBM25,
	}

	// Add all deferred tools
	for name, tool := range deferredTools {
		allTools[name] = tool
	}

	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []ai.Message{
			{
				Role: ai.RoleUser,
				Content: ai.MessageContent{
					Text: stringPtr("I need to get weather information for San Francisco. Can you help me find and use the right tool?"),
				},
			},
		},
		Tools:    allTools,
		MaxSteps: intPtr(10),
	})

	if err != nil {
		log.Printf("Error in BM25 example: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	printToolCalls(result.ToolCalls)
}

func runRegexExample(model ai.LanguageModel) {
	// Create tool search with Regex
	toolSearchRegex := tools.ToolSearchRegex20251119()

	// Create a large catalog of tools that will be deferred
	deferredTools := createLargeToolCatalog()

	// Combine tool search with deferred tools
	allTools := map[string]ai.Tool{
		"toolSearch": toolSearchRegex,
	}

	// Add all deferred tools
	for name, tool := range deferredTools {
		allTools[name] = tool
	}

	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []ai.Message{
			{
				Role: ai.RoleUser,
				Content: ai.MessageContent{
					Text: stringPtr("Find all database-related tools and show me what's available"),
				},
			},
		},
		Tools:    allTools,
		MaxSteps: intPtr(10),
	})

	if err != nil {
		log.Printf("Error in regex example: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	printToolCalls(result.ToolCalls)
}

func createLargeToolCatalog() map[string]ai.Tool {
	// Create a large catalog of mock tools to demonstrate tool search
	// In a real application, these would be actual tools with implementations

	tools := map[string]ai.Tool{
		// Weather tools
		"get_weather_forecast": createMockTool("get_weather_forecast", "Get weather forecast for a location"),
		"get_current_weather": createMockTool("get_current_weather", "Get current weather conditions"),
		"get_weather_alerts": createMockTool("get_weather_alerts", "Get weather alerts and warnings"),

		// Database tools
		"database_query": createMockTool("database_query", "Execute SQL query on database"),
		"database_insert": createMockTool("database_insert", "Insert data into database"),
		"database_update": createMockTool("database_update", "Update database records"),
		"database_delete": createMockTool("database_delete", "Delete database records"),

		// File tools
		"file_read": createMockTool("file_read", "Read contents of a file"),
		"file_write": createMockTool("file_write", "Write contents to a file"),
		"file_delete": createMockTool("file_delete", "Delete a file"),
		"file_search": createMockTool("file_search", "Search for files by pattern"),

		// API tools
		"api_get_request": createMockTool("api_get_request", "Make HTTP GET request"),
		"api_post_request": createMockTool("api_post_request", "Make HTTP POST request"),
		"api_put_request": createMockTool("api_put_request", "Make HTTP PUT request"),

		// User management tools
		"get_user_data": createMockTool("get_user_data", "Retrieve user information"),
		"create_user": createMockTool("create_user", "Create a new user account"),
		"update_user": createMockTool("update_user", "Update user information"),
		"delete_user": createMockTool("delete_user", "Delete user account"),

		// Analytics tools
		"get_analytics_report": createMockTool("get_analytics_report", "Generate analytics report"),
		"track_event": createMockTool("track_event", "Track analytics event"),
		"get_metrics": createMockTool("get_metrics", "Retrieve system metrics"),
	}

	return tools
}

func createMockTool(name, description string) ai.Tool {
	// Create a mock tool with deferred loading enabled
	// In a real application, these would have actual implementations
	return types.Tool{
		Name:        name,
		Description: description,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"param": map[string]interface{}{
					"type":        "string",
					"description": "Generic parameter for mock tool",
				},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{
				"status":  "success",
				"message": fmt.Sprintf("Mock execution of %s", name),
				"input":   input,
			}, nil
		},
		// Mark this tool for deferred loading
		// ProviderExecuted: false means it's executed locally when discovered
	}
}

func printToolCalls(toolCalls []ai.ToolCall) {
	if len(toolCalls) > 0 {
		fmt.Println("\nTool Calls:")
		for i, toolCall := range toolCalls {
			fmt.Printf("  %d. %s\n", i+1, toolCall.ToolName)
			if len(toolCall.Arguments) > 0 {
				fmt.Printf("     Arguments: %v\n", toolCall.Arguments)
			}
		}
	}
}

func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
