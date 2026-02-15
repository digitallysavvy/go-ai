// Package main demonstrates basic chat with Moonshot AI
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

	// Get language model
	model, err := prov.LanguageModel("moonshot-v1-32k")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Generate response
	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful AI assistant.",
		Prompt: "Explain quantum computing in simple terms",
	})

	if err != nil {
		log.Fatalf("Failed to generate: %v", err)
	}

	// Print results
	fmt.Println("Response:", result.Text)
	fmt.Println("\nUsage:")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total tokens: %d\n", result.Usage.GetTotalTokens())
	fmt.Printf("  Finish reason: %s\n", result.FinishReason)
}
