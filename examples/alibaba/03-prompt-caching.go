package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

// Example 3: Prompt Caching
// This example demonstrates using Alibaba's prompt caching to reduce costs

func main() {
	// Create Alibaba provider
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)
	model, err := prov.LanguageModel("qwen-max")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Large system prompt that we want to cache
	systemPrompt := `You are an expert software architect with deep knowledge of:
- Microservices architecture and design patterns
- Cloud-native application development
- Kubernetes and container orchestration
- Database design and optimization
- API design and RESTful principles
- Security best practices
- Performance optimization techniques

When answering questions, provide detailed explanations with code examples where appropriate.
Consider scalability, maintainability, and security in all recommendations.`

	// First call - this will cache the system prompt
	fmt.Println("First call (cache miss expected):")
	prompt1 := types.Prompt{
		System: systemPrompt,
		Text:   "What are the best practices for designing a microservices architecture?",
	}

	result1, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt1,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result1.Text[:200] + "...") // Print first 200 chars
	fmt.Println()
	printCacheUsage("First call", result1.Usage)
	fmt.Println()

	// Second call with same system prompt but different question
	fmt.Println("Second call (cache hit expected):")
	prompt2 := types.Prompt{
		System: systemPrompt,
		Text:   "How should I design my database schema for a multi-tenant SaaS application?",
	}

	result2, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt2,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result2.Text[:200] + "...") // Print first 200 chars
	fmt.Println()
	printCacheUsage("Second call", result2.Usage)
}

func printCacheUsage(label string, usage types.Usage) {
	fmt.Printf("%s Token Usage:\n", label)
	fmt.Printf("  Input:  %d tokens\n", usage.GetInputTokens())
	fmt.Printf("  Output: %d tokens\n", usage.GetOutputTokens())

	if usage.InputDetails != nil {
		if usage.InputDetails.CacheReadTokens != nil && *usage.InputDetails.CacheReadTokens > 0 {
			fmt.Printf("  ✓ Cache Hit:  %d tokens (saved cost!)\n", *usage.InputDetails.CacheReadTokens)
		}
		if usage.InputDetails.CacheWriteTokens != nil && *usage.InputDetails.CacheWriteTokens > 0 {
			fmt.Printf("  → Cache Write: %d tokens (cached for future)\n", *usage.InputDetails.CacheWriteTokens)
		}
	}

	fmt.Printf("  Total:  %d tokens\n", usage.GetTotalTokens())
}
