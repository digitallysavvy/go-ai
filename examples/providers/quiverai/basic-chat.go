package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
)

// This example demonstrates text generation with QuiverAI's Arrow 2 language
// models, which reuse the Open Responses transport under the hood.
// Prerequisites:
// 1. A QuiverAI API key, set via QUIVERAI_API_KEY or quiverai.Config.APIKey.

func main() {
	// Create the QuiverAI provider. APIKey defaults to QUIVERAI_API_KEY and
	// BaseURL defaults to https://api.quiver.ai/v1 (overridable via
	// QUIVERAI_BASE_URL).
	provider := quiverai.New(quiverai.Config{})

	// Get the Arrow 2 language model (quiverai.ModelArrow2Telos is also
	// available for the larger Arrow 2 Telos model).
	model, err := provider.LanguageModel(quiverai.ModelArrow2)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Basic Text Generation with QuiverAI Arrow 2 ===")

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "In one sentence, explain what an SVG icon is.",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:")
	fmt.Println(result.Text)
	fmt.Println("\nUsage:")
	fmt.Printf("  Input tokens:  %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Finish reason: %s\n", result.FinishReason)

	// QuiverAI's reasoning effort/summary are set via
	// providerOptions.quiverai (reasoningEffort: low|medium|high|xhigh,
	// reasoningSummary: auto).
	fmt.Println("\n\n=== With Reasoning Effort ===")

	reasoningResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Suggest a minimal color palette for a weather app icon set.",
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"reasoningEffort":  "medium",
				"reasoningSummary": "auto",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:")
	fmt.Println(reasoningResult.Text)

	// Reasoning content (when the model returns it) is available as
	// types.ReasoningContent parts alongside the assistant's text.
	for _, msg := range reasoningResult.ResponseMessages {
		for _, part := range msg.Content {
			if reasoning, ok := part.(types.ReasoningContent); ok && reasoning.Text != "" {
				fmt.Println("\nReasoning summary:")
				fmt.Println(reasoning.Text)
			}
		}
	}
}
