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

// Example 1: Basic Chat with Qwen
// This example demonstrates simple text generation using Alibaba's Qwen models

func main() {
	// Create Alibaba provider with API key from environment
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Get a language model (qwen-plus is balanced for performance and cost)
	model, err := prov.LanguageModel("qwen-plus")
	if err != nil {
		log.Fatal(err)
	}

	// Create a simple prompt
	prompt := types.Prompt{
		Text: "Write a haiku about artificial intelligence.",
	}

	// Generate text
	ctx := context.Background()
	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the result
	fmt.Println("Generated Haiku:")
	fmt.Println(result.Text)
	fmt.Println()

	// Print token usage
	fmt.Printf("Token Usage:\n")
	fmt.Printf("  Input:  %d tokens\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output: %d tokens\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total:  %d tokens\n", result.Usage.GetTotalTokens())
}
