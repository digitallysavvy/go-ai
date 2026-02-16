package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
)

func main() {
	ctx := context.Background()

	// Create Bedrock Anthropic provider with cache for tools
	ttl := bedrockAnthropic.CacheTTL1Hour
	provider := bedrockAnthropic.New(bedrockAnthropic.Config{
		Region: "us-east-1",
		Credentials: &bedrockAnthropic.AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
		CacheConfig: bedrockAnthropic.NewCacheConfig(
			bedrockAnthropic.WithCacheTTL(ttl),
			bedrockAnthropic.WithSystemCache(),
			bedrockAnthropic.WithToolCache(),
		),
	})

	// Get language model
	model, err := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Define tools that will be cached
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g. San Francisco, CA",
				},
				"unit": map[string]interface{}{
					"type": "string",
					"enum": []string{"celsius", "fahrenheit"},
				},
			},
			"required": []string{"location"},
		},
	}

	calculatorTool := types.Tool{
		Name:        "calculator",
		Description: "Perform basic arithmetic operations",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide"},
				},
				"a": map[string]interface{}{
					"type":        "number",
					"description": "First number",
				},
				"b": map[string]interface{}{
					"type":        "number",
					"description": "Second number",
				},
			},
			"required": []string{"operation", "a", "b"},
		},
	}

	// First request - creates cache for tools
	fmt.Println("=== First request (creating cache for tools with 1h TTL) ===")
	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful assistant with access to weather and calculator tools.",
		Prompt: "What's the weather like in San Francisco?",
		Tools:  []types.Tool{weatherTool, calculatorTool},
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("Response:", result1.Text)
	if len(result1.ToolCalls) > 0 {
		fmt.Println("Tool calls:", result1.ToolCalls)
	}
	fmt.Println()

	// Display cache statistics
	if result1.Usage.InputDetails != nil {
		if result1.Usage.InputDetails.CacheWriteTokens != nil && *result1.Usage.InputDetails.CacheWriteTokens > 0 {
			fmt.Printf("✓ Cache created: %d tokens written to cache (includes tool definitions)\n", *result1.Usage.InputDetails.CacheWriteTokens)
		}
		if result1.Usage.InputDetails.CacheReadTokens != nil && *result1.Usage.InputDetails.CacheReadTokens > 0 {
			fmt.Printf("✓ Cache hits: %d tokens read from cache\n", *result1.Usage.InputDetails.CacheReadTokens)
		}
	}
	fmt.Printf("Total tokens: %d\n", result1.Usage.GetTotalTokens())
	fmt.Println()

	// Second request - uses cached tools
	fmt.Println("=== Second request (using cached tools) ===")
	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful assistant with access to weather and calculator tools.",
		Prompt: "Calculate 42 plus 17",
		Tools:  []types.Tool{weatherTool, calculatorTool},
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("Response:", result2.Text)
	if len(result2.ToolCalls) > 0 {
		fmt.Println("Tool calls:", result2.ToolCalls)
	}
	fmt.Println()

	// Display cache statistics
	if result2.Usage.InputDetails != nil {
		if result2.Usage.InputDetails.CacheWriteTokens != nil && *result2.Usage.InputDetails.CacheWriteTokens > 0 {
			fmt.Printf("✓ Cache created: %d tokens written to cache\n", *result2.Usage.InputDetails.CacheWriteTokens)
		}
		if result2.Usage.InputDetails.CacheReadTokens != nil && *result2.Usage.InputDetails.CacheReadTokens > 0 {
			fmt.Printf("✓ Cache hits: %d tokens read from cache (tool definitions)\n", *result2.Usage.InputDetails.CacheReadTokens)
		}
	}
	fmt.Printf("Total tokens: %d\n", result2.Usage.GetTotalTokens())
	fmt.Println()

	fmt.Println("Note: Caching tools is cost-effective when you have many tools that remain constant")
	fmt.Println("Tool definitions can be large, so caching them reduces both latency and costs")
}
