// Package main demonstrates using GenerateText with ObjectOutput to produce
// a typed Go struct directly from a language model response.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Recipe is the structured output type we want the model to produce.
type Recipe struct {
	Name        string       `json:"name"`
	Ingredients []Ingredient `json:"ingredients"`
	Steps       []string     `json:"steps"`
}

// Ingredient is a sub-type used in Recipe.
type Ingredient struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
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
		Prompt: "Generate a simple lasagna recipe.",
		Output: ai.ObjectOutput[Recipe](ai.ObjectOutputOptions{
			Schema:      ai.SchemaFor[Recipe](),
			Name:        "recipe",
			Description: "A complete recipe with name, ingredients, and steps",
		}),
	})
	if err != nil {
		log.Fatalf("generation failed: %v", err)
	}

	// result.Output is typed as 'any'; type-assert to the concrete struct.
	recipe, ok := result.Output.(Recipe)
	if !ok {
		log.Fatalf("unexpected output type: %T", result.Output)
	}

	fmt.Printf("Recipe: %s\n\n", recipe.Name)

	fmt.Println("Ingredients:")
	for _, ing := range recipe.Ingredients {
		fmt.Printf("  - %s: %s\n", ing.Name, ing.Amount)
	}

	fmt.Println("\nSteps:")
	for i, step := range recipe.Steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}

	fmt.Printf("\nTokens used: input=%v output=%v\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}
