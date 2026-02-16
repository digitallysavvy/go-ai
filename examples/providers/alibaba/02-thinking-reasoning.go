//go:build ignore
// +build ignore

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

// Example 2: Thinking/Reasoning with Qwen QwQ
// This example demonstrates using Alibaba's reasoning model with thinking budget

func main() {
	// Create Alibaba provider
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Use the reasoning model (qwen-qwq-32b-preview)
	model, err := prov.LanguageModel("qwen-qwq-32b-preview")
	if err != nil {
		log.Fatal(err)
	}

	// Create a complex problem that benefits from reasoning
	prompt := types.Prompt{
		Text: `Solve this logic puzzle:
Three friends - Alice, Bob, and Carol - each have a different pet (dog, cat, fish).
- Alice doesn't have a dog
- Bob is allergic to cats
- Carol lives in an apartment that doesn't allow fish
Who has which pet?`,
	}

	// Generate with thinking enabled
	ctx := context.Background()
	thinkingBudget := 1000 // Allow up to 1000 thinking tokens

	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt,
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"enable_thinking":  true,
				"thinking_budget": thinkingBudget,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the result
	fmt.Println("Solution:")
	fmt.Println(result.Text)
	fmt.Println()

	// Print token usage including thinking tokens
	fmt.Printf("Token Usage:\n")
	fmt.Printf("  Input:      %d tokens\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output:     %d tokens\n", result.Usage.GetOutputTokens())

	if result.Usage.OutputDetails != nil && result.Usage.OutputDetails.ReasoningTokens != nil {
		fmt.Printf("  Reasoning:  %d tokens\n", *result.Usage.OutputDetails.ReasoningTokens)
		if result.Usage.OutputDetails.TextTokens != nil {
			fmt.Printf("  Text Only:  %d tokens\n", *result.Usage.OutputDetails.TextTokens)
		}
	}

	fmt.Printf("  Total:      %d tokens\n", result.Usage.GetTotalTokens())
}
