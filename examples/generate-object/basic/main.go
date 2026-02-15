package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Recipe represents a recipe with ingredients and steps
type Recipe struct {
	Name        string       `json:"name"`
	Ingredients []Ingredient `json:"ingredients"`
	Steps       []string     `json:"steps"`
	PrepTime    int          `json:"prepTime"` // in minutes
	CookTime    int          `json:"cookTime"` // in minutes
}

// Ingredient represents a single ingredient with amount
type Ingredient struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI provider
	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Create language model
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: Generate a simple recipe
	fmt.Println("=== Example 1: Generate Lasagna Recipe ===")
	generateLasagnaRecipe(ctx, model)

	// Example 2: Generate a recipe with specific requirements
	fmt.Println("\n=== Example 2: Generate Vegan Recipe ===")
	generateVeganRecipe(ctx, model)

	// Example 3: Generate multiple recipes
	fmt.Println("\n=== Example 3: Generate Multiple Recipes ===")
	generateMultipleRecipes(ctx, model)
}

func generateLasagnaRecipe(ctx context.Context, model provider.LanguageModel) {
	// Define the JSON schema for a recipe
	recipeSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "The name of the recipe",
			},
			"ingredients": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Name of the ingredient",
						},
						"amount": map[string]interface{}{
							"type":        "string",
							"description": "Amount of the ingredient (e.g., '2 cups', '1 lb')",
						},
					},
					"required": []string{"name", "amount"},
				},
			},
			"steps": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Step-by-step cooking instructions",
			},
			"prepTime": map[string]interface{}{
				"type":        "integer",
				"description": "Preparation time in minutes",
			},
			"cookTime": map[string]interface{}{
				"type":        "integer",
				"description": "Cooking time in minutes",
			},
		},
		"required": []string{"name", "ingredients", "steps", "prepTime", "cookTime"},
	})

	// Generate the recipe
	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a lasagna recipe.",
		Schema: recipeSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	// Parse the result into our Recipe struct
	var recipe Recipe
	jsonBytes, _ := json.Marshal(result.Object)
	if err := json.Unmarshal(jsonBytes, &recipe); err != nil {
		log.Printf("Error parsing recipe: %v", err)
		return
	}

	// Pretty print the recipe
	fmt.Printf("Recipe: %s\n", recipe.Name)
	fmt.Printf("Prep Time: %d minutes\n", recipe.PrepTime)
	fmt.Printf("Cook Time: %d minutes\n", recipe.CookTime)
	fmt.Printf("\nIngredients:\n")
	for _, ing := range recipe.Ingredients {
		fmt.Printf("  - %s: %s\n", ing.Name, ing.Amount)
	}
	fmt.Printf("\nSteps:\n")
	for i, step := range recipe.Steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}

	fmt.Printf("\nToken usage: %d (input: %d, output: %d)\n",
		result.Usage.GetTotalTokens(),
		result.Usage.GetInputTokens(),
		result.Usage.GetOutputTokens())
}

func generateVeganRecipe(ctx context.Context, model provider.LanguageModel) {
	recipeSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
			"ingredients": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":   map[string]interface{}{"type": "string"},
						"amount": map[string]interface{}{"type": "string"},
					},
					"required": []string{"name", "amount"},
				},
			},
			"steps": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"prepTime": map[string]interface{}{"type": "integer"},
			"cookTime": map[string]interface{}{"type": "integer"},
		},
		"required": []string{"name", "ingredients", "steps"},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a vegan pasta recipe that's quick to make (under 30 minutes).",
		Schema: recipeSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	// Pretty print JSON
	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}

func generateMultipleRecipes(ctx context.Context, model provider.LanguageModel) {
	recipesSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"recipes": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Recipe name",
						},
						"cuisine": map[string]interface{}{
							"type":        "string",
							"description": "Type of cuisine (e.g., Italian, Mexican, Chinese)",
						},
						"difficulty": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"easy", "medium", "hard"},
							"description": "Difficulty level",
						},
						"ingredients": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"name":   map[string]interface{}{"type": "string"},
									"amount": map[string]interface{}{"type": "string"},
								},
								"required": []string{"name", "amount"},
							},
						},
						"steps": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"name", "cuisine", "difficulty", "ingredients", "steps"},
				},
				"minItems": 3,
				"maxItems": 3,
			},
		},
		"required": []string{"recipes"},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate 3 different dinner recipes: one easy, one medium difficulty, and one hard. Include diverse cuisines.",
		Schema: recipesSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	// Pretty print the result
	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
}
