// Package main demonstrates using GenerateText with ArrayOutput to produce
// a typed slice from a language model response.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Hero is a single array element type.
type Hero struct {
	Name        string `json:"name"`
	Class       string `json:"class"`
	Description string `json:"description"`
}

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("failed to create model: %v", err)
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Generate 3 unique hero descriptions for a fantasy role-playing game.",
		Output: ai.ArrayOutput[Hero](ai.ArrayOutputOptions[Hero]{
			ElementSchema: ai.SchemaFor[Hero](),
			Name:          "heroes",
			Description:   "A list of fantasy RPG heroes",
		}),
	})
	if err != nil {
		log.Fatalf("generation failed: %v", err)
	}

	// result.Output is typed as 'any'; type-assert to the concrete slice.
	heroes, ok := result.Output.([]Hero)
	if !ok {
		log.Fatalf("unexpected output type: %T", result.Output)
	}

	fmt.Printf("Generated %d heroes:\n\n", len(heroes))
	for i, hero := range heroes {
		fmt.Printf("%d. %s (%s)\n   %s\n\n", i+1, hero.Name, hero.Class, hero.Description)
	}

	fmt.Printf("Tokens used: input=%v output=%v\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}
