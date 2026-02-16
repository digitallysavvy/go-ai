// Package main demonstrates prompt caching with Moonshot AI
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
)

func main() {
	// Create Moonshot provider
	cfg, err := moonshot.NewConfig("") // Uses MOONSHOT_API_KEY env var
	if err != nil {
		log.Fatalf("Failed to create config: %v", err)
	}

	prov := moonshot.New(cfg)

	// Get 128k model (best for long context caching)
	model, err := prov.LanguageModel("moonshot-v1-128k")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Large system prompt that benefits from caching
	largeSystemPrompt := `You are an expert AI assistant specializing in computer science and software engineering.
You have deep knowledge of:
- Algorithms and data structures
- System design and architecture
- Programming languages (Go, Python, JavaScript, Rust, C++)
- Distributed systems and microservices
- Database design (SQL and NoSQL)
- Cloud computing (AWS, GCP, Azure)
- DevOps and CI/CD
- Security best practices
- Performance optimization
When answering questions, provide detailed explanations with examples.`

	ctx := context.Background()

	// First request - cache miss
	fmt.Println("=== First Request (Cache Miss) ===")
	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: largeSystemPrompt,
		Prompt: "What is a binary search tree?",
	})

	if err != nil {
		log.Fatalf("Failed to generate: %v", err)
	}

	if len(result1.Text) > 100 {
		fmt.Println("Response:", result1.Text[:100]+"...")
	} else {
		fmt.Println("Response:", result1.Text)
	}
	printCacheUsage(result1)

	// Second request with same system prompt - cache hit
	fmt.Println("\n=== Second Request (Cache Hit Expected) ===")
	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: largeSystemPrompt,
		Prompt: "What is the time complexity of binary search?",
	})
	if err != nil {
		log.Fatalf("Failed to generate: %v", err)
	}

	if len(result2.Text) > 100 {
		fmt.Println("Response:", result2.Text[:100]+"...")
	} else {
		fmt.Println("Response:", result2.Text)
	}
	printCacheUsage(result2)
}

func printCacheUsage(result *ai.GenerateTextResult) {
	fmt.Println("\nToken Usage:")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())

	if result.Usage.InputDetails != nil {
		if result.Usage.InputDetails.CacheReadTokens != nil {
			fmt.Printf("  Cache hits: %d tokens\n", *result.Usage.InputDetails.CacheReadTokens)
		}
		if result.Usage.InputDetails.NoCacheTokens != nil {
			fmt.Printf("  No cache: %d tokens\n", *result.Usage.InputDetails.NoCacheTokens)
		}
	}

	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total tokens: %d\n", result.Usage.GetTotalTokens())
}
