// Recipe: get typed data from a model.
//
// Run: OPENAI_API_KEY=... go run ./examples/recipes/structured-output
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// The schema comes from the struct's JSON tags.
type Recipe struct {
	Name        string   `json:"name"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
}

func main() {
	model, err := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}).
		LanguageModel(openai.ModelGPT6Astra)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Give me a lasagna recipe.",
		Output: ai.ObjectOutput[Recipe](ai.ObjectOutputOptions{
			Schema:      ai.SchemaFor[Recipe](),
			Name:        "recipe",
			Description: "A recipe with a name, ingredients and steps",
		}),
	})
	if err != nil {
		log.Fatal(err)
	}

	recipe := result.Output.(Recipe)
	fmt.Println(recipe.Name)
	for i, step := range recipe.Steps {
		fmt.Printf("%d. %s\n", i+1, step)
	}
}
