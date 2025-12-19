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

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Example 1: Generate Character Object ===")
	generateCharacters(ctx, model)

	fmt.Println("\n=== Example 2: Generate Product Catalog ===")
	generateProductCatalog(ctx, model)

	fmt.Println("\n=== Example 3: Generate Task List ===")
	generateTaskList(ctx, model)
}

func generateCharacters(ctx context.Context, model provider.LanguageModel) {
	characterSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"characters": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type": "string",
						},
						"class": map[string]interface{}{
							"type": "string",
							"enum": []string{"warrior", "mage", "rogue", "cleric"},
						},
						"level": map[string]interface{}{
							"type":    "integer",
							"minimum": 1,
							"maximum": 20,
						},
						"description": map[string]interface{}{
							"type": "string",
						},
						"abilities": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"name", "class", "level", "description"},
				},
			},
		},
	})

	fmt.Println("Generating characters...")

	// Use StreamObject with OnFinish callback
	result, err := ai.StreamObject(ctx, ai.StreamObjectOptions{
		Model:  model,
		Prompt: "Generate 3 fantasy RPG characters with unique abilities and backstories",
		Schema: characterSchema,
		OnFinish: func(ctx context.Context, result *ai.GenerateObjectResult, userContext interface{}) {
			fmt.Println("✓ Generation complete!")
		},
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("\nGenerated Characters:")
	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nToken usage: %d\n", result.Usage.TotalTokens)
}

func generateProductCatalog(ctx context.Context, model provider.LanguageModel) {
	productSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"products": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type": "string",
						},
						"name": map[string]interface{}{
							"type": "string",
						},
						"category": map[string]interface{}{
							"type": "string",
						},
						"price": map[string]interface{}{
							"type":    "number",
							"minimum": 0,
						},
						"description": map[string]interface{}{
							"type": "string",
						},
						"features": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"inStock": map[string]interface{}{
							"type": "boolean",
						},
					},
				},
			},
		},
	})

	fmt.Println("Generating product catalog...")

	result, err := ai.StreamObject(ctx, ai.StreamObjectOptions{
		Model:  model,
		Prompt: "Generate a catalog of 4 tech products (laptops, phones, tablets) with realistic specs",
		Schema: productSchema,
		OnFinish: func(ctx context.Context, result *ai.GenerateObjectResult, userContext interface{}) {
			fmt.Println("✓ Catalog generated!")
		},
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("\nProduct Catalog:")
	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
}

func generateTaskList(ctx context.Context, model provider.LanguageModel) {
	taskSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"project": map[string]interface{}{
				"type": "string",
			},
			"tasks": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type": "string",
						},
						"title": map[string]interface{}{
							"type": "string",
						},
						"description": map[string]interface{}{
							"type": "string",
						},
						"priority": map[string]interface{}{
							"type": "string",
							"enum": []string{"low", "medium", "high", "critical"},
						},
						"estimatedHours": map[string]interface{}{
							"type":    "integer",
							"minimum": 1,
						},
						"dependencies": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
	})

	fmt.Println("Generating task list...")

	result, err := ai.StreamObject(ctx, ai.StreamObjectOptions{
		Model:  model,
		Prompt: "Create a project plan for building a mobile app with 5 tasks including dependencies",
		Schema: taskSchema,
		OnFinish: func(ctx context.Context, result *ai.GenerateObjectResult, userContext interface{}) {
			fmt.Println("✓ Task list generated!")
		},
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("\nTask List:")
	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}
