// Package main demonstrates basic chat with the Z.AI provider.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

func main() {
	// Create the Z.AI provider. Uses the ZAI_API_KEY environment variable
	// when Config.APIKey is empty.
	prov := zai.New(zai.Config{})

	model, err := prov.LanguageModel(zai.ModelGLM46)
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful AI assistant.",
		Prompt: "Explain what a GLM model is in one sentence.",
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
