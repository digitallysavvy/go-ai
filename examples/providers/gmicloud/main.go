// Package main demonstrates basic chat with the GMI Cloud provider.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
)

func main() {
	// Create the GMI Cloud provider. Uses the GMI_CLOUD_APIKEY environment
	// variable when Config.APIKey is empty.
	prov := gmicloud.New(gmicloud.Config{})

	model, err := prov.LanguageModel(gmicloud.ModelDeepSeekV4FlashChat)
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful AI assistant.",
		Prompt: "Explain what GPU inference is in one sentence.",
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
