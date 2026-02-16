package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
)

func main() {
	ctx := context.Background()

	// Create Bedrock Anthropic provider with 1-hour cache TTL
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
		),
	})

	// Get language model - requires Claude 4.5 Sonnet v2, Opus, or Haiku
	model, err := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Large context that we want to cache with 1-hour TTL
	largeContext := strings.Repeat("This is a large document that should be cached for 1 hour. ", 1000)

	// First request - creates cache
	fmt.Println("=== First request (creating cache with 1h TTL) ===")
	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: largeContext,
		Prompt: "What is the main topic of this document?",
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("Response:", result1.Text)
	fmt.Println()

	// Display cache statistics
	if result1.Usage.InputDetails != nil {
		if result1.Usage.InputDetails.CacheWriteTokens != nil && *result1.Usage.InputDetails.CacheWriteTokens > 0 {
			fmt.Printf("✓ Cache created: %d tokens written to cache (1h TTL)\n", *result1.Usage.InputDetails.CacheWriteTokens)
		}
		if result1.Usage.InputDetails.CacheReadTokens != nil && *result1.Usage.InputDetails.CacheReadTokens > 0 {
			fmt.Printf("✓ Cache hits: %d tokens read from cache\n", *result1.Usage.InputDetails.CacheReadTokens)
		}
	}
	fmt.Printf("Total tokens: %d\n", result1.Usage.GetTotalTokens())
	fmt.Println()

	// Second request - uses cache (within 1 hour)
	fmt.Println("=== Second request (using cache) ===")
	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: largeContext,
		Prompt: "Summarize this document in one sentence.",
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("Response:", result2.Text)
	fmt.Println()

	// Display cache statistics
	if result2.Usage.InputDetails != nil {
		if result2.Usage.InputDetails.CacheWriteTokens != nil && *result2.Usage.InputDetails.CacheWriteTokens > 0 {
			fmt.Printf("✓ Cache created: %d tokens written to cache\n", *result2.Usage.InputDetails.CacheWriteTokens)
		}
		if result2.Usage.InputDetails.CacheReadTokens != nil && *result2.Usage.InputDetails.CacheReadTokens > 0 {
			fmt.Printf("✓ Cache hits: %d tokens read from cache\n", *result2.Usage.InputDetails.CacheReadTokens)
		}
	}
	fmt.Printf("Total tokens: %d\n", result2.Usage.GetTotalTokens())
	fmt.Println()

	fmt.Println("Note: 1-hour cache TTL is ideal for longer sessions with repeated context")
	fmt.Println("Supported by Claude 4.5 Sonnet v2, Claude 4.5 Opus, and Claude 4.5 Haiku")
	fmt.Println("Model IDs:")
	fmt.Println("  - us.anthropic.claude-4-5-sonnet-v2:0")
	fmt.Println("  - us.anthropic.claude-4-5-opus-20250514:0")
	fmt.Println("  - us.anthropic.claude-4-5-haiku-20250510:0")
}
