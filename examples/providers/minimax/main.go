// Package main demonstrates basic chat with the MiniMax provider, including
// MiniMax's adaptive thinking provider option.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
)

func main() {
	// Create the MiniMax provider. Uses the MINIMAX_API_KEY environment
	// variable when Config.APIKey is empty.
	prov := minimax.New(minimax.Config{})

	model, err := prov.LanguageModel(minimax.ModelM3)
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful AI assistant.",
		Prompt: "How many letters 'r' are in the word strawberry?",
		ProviderOptions: map[string]interface{}{
			"minimax": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "adaptive"},
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate: %v", err)
	}

	fmt.Println("Response:", result.Text)
	fmt.Println("\nUsage:")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total tokens: %d\n", result.Usage.GetTotalTokens())
	fmt.Printf("  Finish reason: %s\n", result.FinishReason)
}
