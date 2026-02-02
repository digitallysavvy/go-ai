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

	// Create Bedrock Anthropic provider
	provider := bedrockAnthropic.New(bedrockAnthropic.Config{
		Region: "us-east-1",
		Credentials: &bedrockAnthropic.AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
	})

	// Get language model
	model, err := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Large context that we want to cache
	largeContext := strings.Repeat("This is a large document that should be cached. ", 1000)

	// First request - creates cache
	fmt.Println("First request (creating cache)...")
	result1, err := ai.GenerateText(ctx, ai.GenerateOptions{
		Model: model,
		Prompt: fmt.Sprintf("%s\n\nQuestion: What is the main topic of this document?", largeContext),
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("Response:", result1.Text)
	fmt.Println()

	// Display cache statistics
	if result1.Usage.InputDetails != nil {
		if result1.Usage.InputDetails.CacheWriteTokens != nil && *result1.Usage.InputDetails.CacheWriteTokens > 0 {
			fmt.Printf("Cache created: %d tokens written to cache\n", *result1.Usage.InputDetails.CacheWriteTokens)
		}
		if result1.Usage.InputDetails.CacheReadTokens != nil {
			fmt.Printf("Cache hits: %d tokens read from cache\n", *result1.Usage.InputDetails.CacheReadTokens)
		}
	}
	fmt.Printf("Total tokens: %d\n", result1.Usage.GetTotalTokens())
	fmt.Println()

	// Second request - uses cache
	fmt.Println("Second request (using cache)...")
	result2, err := ai.GenerateText(ctx, ai.GenerateOptions{
		Model: model,
		Prompt: fmt.Sprintf("%s\n\nQuestion: Summarize this document in one sentence.", largeContext),
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("Response:", result2.Text)
	fmt.Println()

	// Display cache statistics
	if result2.Usage.InputDetails != nil {
		if result2.Usage.InputDetails.CacheWriteTokens != nil && *result2.Usage.InputDetails.CacheWriteTokens > 0 {
			fmt.Printf("Cache created: %d tokens written to cache\n", *result2.Usage.InputDetails.CacheWriteTokens)
		}
		if result2.Usage.InputDetails.CacheReadTokens != nil {
			fmt.Printf("Cache hits: %d tokens read from cache\n", *result2.Usage.InputDetails.CacheReadTokens)
		}
	}
	fmt.Printf("Total tokens: %d\n", result2.Usage.GetTotalTokens())
	fmt.Println()

	fmt.Println("Note: Prompt caching can significantly reduce latency and costs for repeated requests with large contexts")
}
