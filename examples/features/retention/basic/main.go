package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	ctx := context.Background()

	// Create OpenAI provider
	provider := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Get a language model
	model, err := provider.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	fmt.Println("=== Example 1: Default behavior (retain everything) ===")
	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Say hello in one word",
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Text: %s\n", result1.Text)
	fmt.Printf("RawRequest present: %v\n", result1.RawRequest != nil)
	fmt.Printf("RawResponse present: %v\n", result1.RawResponse != nil)
	fmt.Printf("Usage: %d total tokens\n\n", result1.Usage.GetTotalTokens())

	fmt.Println("=== Example 2: Memory optimization (exclude request & response) ===")

	// Enable memory optimization by excluding request/response bodies
	retention := &types.RetentionSettings{
		RequestBody:  types.BoolPtr(false), // Don't retain request
		ResponseBody: types.BoolPtr(false), // Don't retain response
	}

	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:                 model,
		Prompt:                "Explain quantum computing in one sentence",
		ExperimentalRetention: retention,
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Text: %s\n", result2.Text)
	fmt.Printf("RawRequest present: %v\n", result2.RawRequest != nil)
	fmt.Printf("RawResponse present: %v\n", result2.RawResponse != nil)
	fmt.Printf("Usage: %d total tokens\n\n", result2.Usage.GetTotalTokens())

	fmt.Println("=== Example 3: Exclude only request body ===")

	result3, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is AI?",
		ExperimentalRetention: &types.RetentionSettings{
			RequestBody: types.BoolPtr(false), // Don't retain request
			// ResponseBody is nil, so it will be retained by default
		},
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Text: %s\n", result3.Text)
	fmt.Printf("RawRequest present: %v\n", result3.RawRequest != nil)
	fmt.Printf("RawResponse present: %v\n", result3.RawResponse != nil)
	fmt.Printf("Usage: %d total tokens\n\n", result3.Usage.GetTotalTokens())

	fmt.Println("✅ All retention examples completed successfully!")
	fmt.Println("\nKey benefits of retention settings:")
	fmt.Println("- 50-80% memory reduction for image-heavy workloads")
	fmt.Println("- Improved privacy by not storing sensitive data")
	fmt.Println("- Lower memory pressure = better garbage collection")
	fmt.Println("- Opt-in: backwards compatible with existing code")
}
