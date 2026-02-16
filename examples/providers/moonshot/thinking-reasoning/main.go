// Package main demonstrates thinking/reasoning with Kimi K2.5
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

	// Get K2.5 model (supports thinking)
	model, err := prov.LanguageModel("kimi-k2.5")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Generate response with thinking enabled
	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful AI assistant that shows your reasoning process.",
		Prompt: "Solve this problem step by step: If a train travels 120 km in 2 hours, then stops for 30 minutes, then travels another 180 km in 3 hours, what is its average speed for the entire journey?",
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{
				"thinking": map[string]interface{}{
					"type":          "enabled",
					"budget_tokens": 500,
				},
				"reasoning_history": "interleaved",
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate: %v", err)
	}

	// Print results
	fmt.Println("Response:", result.Text)
	fmt.Println("\nToken Usage:")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())

	// Check reasoning tokens
	if result.Usage.OutputDetails != nil && result.Usage.OutputDetails.ReasoningTokens != nil {
		fmt.Printf("  Reasoning tokens: %d\n", *result.Usage.OutputDetails.ReasoningTokens)
		if result.Usage.OutputDetails.TextTokens != nil {
			fmt.Printf("  Text tokens: %d\n", *result.Usage.OutputDetails.TextTokens)
		}
	}

	fmt.Printf("  Total tokens: %d\n", result.Usage.GetTotalTokens())
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}
