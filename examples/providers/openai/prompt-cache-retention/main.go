package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	ctx := context.Background()

	fmt.Println("=== Example 1: Default Cache Retention (in_memory) ===")
	defaultCacheExample(ctx, p)

	fmt.Println("\n=== Example 2: Extended Cache Retention (24h) ===")
	extendedCacheExample(ctx, p)

	fmt.Println("\n=== Example 3: Repeated Calls with 24h Cache ===")
	repeatedCallsExample(ctx, p)
}

func defaultCacheExample(ctx context.Context, p *openai.Provider) {
	// Default caching behavior - cache is retained in memory
	model, err := p.LanguageModel("gpt-5.1")
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: "Explain what prompt caching is in AI systems.",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Response:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage:\n")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())

	// Check for cached tokens
	if result.Usage.InputDetails != nil && result.Usage.InputDetails.CacheReadTokens != nil {
		fmt.Printf("  Cache read tokens: %d\n", *result.Usage.InputDetails.CacheReadTokens)
	}
}

func extendedCacheExample(ctx context.Context, p *openai.Provider) {
	// Use extended 24h cache retention for gpt-5.1 series
	// This keeps cached prefixes active for up to 24 hours
	model, err := p.LanguageModel("gpt-5.1")
	if err != nil {
		log.Fatal(err)
	}

	// Large prompt that will benefit from caching
	largePrompt := `You are a helpful AI assistant with extensive knowledge about software engineering.

Context about our project:
- We're building a distributed microservices architecture
- Using Go for backend services
- React for frontend
- PostgreSQL for database
- Redis for caching
- Kubernetes for orchestration
- Our team follows test-driven development
- We use CI/CD with GitHub Actions
- All services use RESTful APIs
- Authentication via JWT tokens
- Monitoring with Prometheus and Grafana

Based on this context, answer the following question:`

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: largePrompt + "\n\nWhat are the best practices for implementing health checks in our microservices?",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"promptCacheRetention": "24h",
			},
		},
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Response:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage:\n")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())

	// With 24h caching, subsequent calls with the same prefix will use cached tokens
	if result.Usage.InputDetails != nil && result.Usage.InputDetails.CacheReadTokens != nil {
		fmt.Printf("  Cache read tokens: %d\n", *result.Usage.InputDetails.CacheReadTokens)
	}
}

func repeatedCallsExample(ctx context.Context, p *openai.Provider) {
	// Demonstrate the benefit of 24h cache retention with repeated calls
	model, err := p.LanguageModel("gpt-5.1")
	if err != nil {
		log.Fatal(err)
	}

	// Large common prefix that will be cached
	commonPrefix := `You are an expert software architect.

Our system architecture:
- Microservices architecture with 15+ services
- Event-driven communication using Kafka
- CQRS pattern for read/write separation
- Event sourcing for audit trail
- API Gateway for external access
- Service mesh with Istio
- Multi-region deployment
- Database per service pattern
- Saga pattern for distributed transactions

Given this architecture, `

	questions := []string{
		"what are the main challenges with event sourcing?",
		"how should we handle distributed tracing?",
		"what's the best way to implement API versioning?",
	}

	for i, question := range questions {
		fmt.Printf("\n--- Call %d: %s ---\n", i+1, question)

		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model: model,
			Prompt: commonPrefix + question,
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{
					"promptCacheRetention": "24h",
				},
			},
		})
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		fmt.Println("Response (truncated):")
		if len(result.Text) > 200 {
			fmt.Println(result.Text[:200] + "...")
		} else {
			fmt.Println(result.Text)
		}

		fmt.Printf("\nToken usage:\n")
		fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())

		// Subsequent calls should show cache hits
		if result.Usage.InputDetails != nil {
			if result.Usage.InputDetails.CacheReadTokens != nil {
				fmt.Printf("  Cache read tokens: %d (saved cost!)\n", *result.Usage.InputDetails.CacheReadTokens)
			}
			if result.Usage.InputDetails.NoCacheTokens != nil {
				fmt.Printf("  No-cache tokens: %d\n", *result.Usage.InputDetails.NoCacheTokens)
			}
		}

		// Small delay between calls
		if i < len(questions)-1 {
			time.Sleep(time.Second)
		}
	}

	fmt.Println("\nNote: The first call writes to cache, subsequent calls read from cache.")
	fmt.Println("With 24h retention, the cache remains active even if calls are hours apart!")
}

// Example with provider.GenerateOptions (lower-level API)
func lowLevelExample(ctx context.Context, p *openai.Provider) {
	model, err := p.LanguageModel("gpt-5.1")
	if err != nil {
		log.Fatal(err)
	}

	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{
							Text: "What is the capital of France?",
						},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"promptCacheRetention": "24h",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
