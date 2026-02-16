package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("claude-sonnet-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Example 1: Cache Large System Prompt ===")
	cacheLargeSystemPrompt(ctx, model)

	fmt.Println("\n=== Example 2: Cache Document Context ===")
	cacheDocumentContext(ctx, model)

	fmt.Println("\n=== Example 3: Multiple Queries with Same Context ===")
	multipleQueriesWithCache(ctx, model)
}

func cacheLargeSystemPrompt(ctx context.Context, model provider.LanguageModel) {
	// Large system prompt that will be cached
	largeSystemPrompt := `You are an expert software architect with deep knowledge of:

1. System Design Patterns:
   - Microservices architecture
   - Event-driven architecture
   - Domain-driven design
   - CQRS and Event Sourcing
   - API Gateway patterns
   - Service mesh architectures

2. Scalability Principles:
   - Horizontal vs vertical scaling
   - Load balancing strategies
   - Caching strategies (Redis, Memcached)
   - Database sharding and replication
   - CDN optimization
   - Async processing patterns

3. Best Practices:
   - 12-factor app methodology
   - Infrastructure as Code
   - CI/CD pipelines
   - Monitoring and observability
   - Security best practices
   - Performance optimization

When answering questions, always:
- Provide practical, production-ready solutions
- Consider scalability and maintainability
- Include trade-offs and alternatives
- Reference industry standards
- Give code examples when relevant`

	// First request - system prompt will be cached
	fmt.Println("First request (caching system prompt)...")
	start := time.Now()

	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: largeSystemPrompt,
		Prompt: "How should I design a microservices architecture for an e-commerce platform?",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	duration1 := time.Since(start)
	fmt.Printf("Response: %s\n", result1.Text[:200]+"...")
	fmt.Printf("Time: %v\n", duration1)
	fmt.Printf("Tokens: %d\n", result1.Usage.TotalTokens)

	// Second request - uses cached system prompt
	fmt.Println("\nSecond request (using cached system prompt)...")
	start = time.Now()

	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: largeSystemPrompt,
		Prompt: "What caching strategy should I use for user sessions?",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	duration2 := time.Since(start)
	fmt.Printf("Response: %s\n", result2.Text[:200]+"...")
	fmt.Printf("Time: %v (faster due to caching)\n", duration2)
	fmt.Printf("Tokens: %d\n", result2.Usage.TotalTokens)
	fmt.Printf("Speed improvement: %.2fx\n", float64(duration1)/float64(duration2))
}

func cacheDocumentContext(ctx context.Context, model provider.LanguageModel) {
	// Large document that will be cached
	document := `# Go Programming Language Specification

## Introduction
Go is a statically typed, compiled programming language designed at Google.
It is syntactically similar to C, but with memory safety, garbage collection,
structural typing, and CSP-style concurrency.

## Types
Go has several built-in types:
- Basic types: bool, string, int, int8, int16, int32, int64, uint, uint8, etc.
- Aggregate types: array, struct
- Reference types: pointer, slice, map, function, channel
- Interface types

## Functions
Functions are first-class values in Go. They can be assigned to variables,
passed as arguments, and returned from other functions.

Example:
func add(x int, y int) int {
    return x + y
}

## Goroutines
A goroutine is a lightweight thread managed by the Go runtime. Use the 'go'
keyword to start a new goroutine:

go myFunction()

## Channels
Channels are typed conduits for communication between goroutines:

ch := make(chan int)
go func() {
    ch <- 42
}()
value := <-ch

[... many more sections of documentation ...]`

	systemPrompt := fmt.Sprintf("You are a Go programming expert. Use this documentation as reference:\n\n%s", document)

	// First query with document
	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: systemPrompt,
		Prompt: "Explain how goroutines work in Go",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Answer: %s\n", result1.Text[:200]+"...")
	fmt.Printf("Tokens: %d (document cached)\n", result1.Usage.TotalTokens)

	// Second query - reuses cached document
	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: systemPrompt,
		Prompt: "How do channels work in Go?",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("\nAnswer: %s\n", result2.Text[:200]+"...")
	fmt.Printf("Tokens: %d (using cached document)\n", result2.Usage.TotalTokens)
}

func multipleQueriesWithCache(ctx context.Context, model provider.LanguageModel) {
	systemPrompt := `You are a helpful AI assistant specializing in data analysis.
You have access to a large dataset and should answer questions about it accurately.`

	queries := []string{
		"What trends do you see in the data?",
		"What are the key insights?",
		"What recommendations would you make?",
	}

	for i, query := range queries {
		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			System: systemPrompt,
			Prompt: query,
		})
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		fmt.Printf("Query %d: %s\n", i+1, query)
		fmt.Printf("Answer: %s\n", result.Text[:150]+"...")
		fmt.Printf("Tokens: %d\n\n", result.Usage.GetTotalTokens())
	}
}
