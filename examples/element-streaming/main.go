package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// TodoItem represents a single todo item
type TodoItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	Category    string `json:"category"`
}

// Product represents a product for an e-commerce site
type Product struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	Category    string  `json:"category"`
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

	// Example 1: Stream todo items
	fmt.Println("=== Example 1: Stream Todo Items ===")
	streamTodoItems(ctx, model)

	// Example 2: Stream products
	fmt.Println("\n=== Example 2: Stream Products ===")
	streamProducts(ctx, model)

	// Example 3: Element streaming with callbacks
	fmt.Println("\n=== Example 3: Element Streaming with Callbacks ===")
	streamWithCallbacks(ctx, model)
}

// streamTodoItems demonstrates basic element streaming
func streamTodoItems(ctx context.Context, model provider.LanguageModel) {
	// Define the schema for a todo item
	todoSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":       map[string]string{"type": "string"},
			"description": map[string]string{"type": "string"},
			"priority":    map[string]string{"type": "integer"},
			"category":    map[string]string{"type": "string"},
		},
		"required": []string{"title", "description", "priority", "category"},
	})

	// Start streaming
	result, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: "Generate a list of 5 todo items for a software developer",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create element stream
	elementCh := ai.ElementStream[TodoItem](result, ai.ElementStreamOptions[TodoItem]{
		ElementSchema: todoSchema,
	})

	// Process elements as they arrive
	fmt.Println("Receiving todo items as they complete...")
	for elem := range elementCh {
		fmt.Printf("\n[%d] %s\n", elem.Index+1, elem.Element.Title)
		fmt.Printf("    Description: %s\n", elem.Element.Description)
		fmt.Printf("    Priority: %d\n", elem.Element.Priority)
		fmt.Printf("    Category: %s\n", elem.Element.Category)
	}
}

// streamProducts demonstrates element streaming with products
func streamProducts(ctx context.Context, model provider.LanguageModel) {
	// Define the schema for a product
	productSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":        map[string]string{"type": "string"},
			"description": map[string]string{"type": "string"},
			"price":       map[string]string{"type": "number"},
			"category":    map[string]string{"type": "string"},
		},
		"required": []string{"name", "description", "price", "category"},
	})

	// Start streaming
	result, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: "Generate a list of 3 electronic products for an online store",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create element stream
	elementCh := ai.ElementStream[Product](result, ai.ElementStreamOptions[Product]{
		ElementSchema: productSchema,
	})

	// Process elements as they arrive
	fmt.Println("Receiving products as they complete...")
	for elem := range elementCh {
		fmt.Printf("\n[%d] %s - $%.2f\n", elem.Index+1, elem.Element.Name, elem.Element.Price)
		fmt.Printf("    %s\n", elem.Element.Description)
		fmt.Printf("    Category: %s\n", elem.Element.Category)
	}
}

// streamWithCallbacks demonstrates element streaming with callbacks
func streamWithCallbacks(ctx context.Context, model provider.LanguageModel) {
	// Define the schema for a todo item
	todoSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":       map[string]string{"type": "string"},
			"description": map[string]string{"type": "string"},
			"priority":    map[string]string{"type": "integer"},
			"category":    map[string]string{"type": "string"},
		},
		"required": []string{"title", "description", "priority", "category"},
	})

	// Start streaming
	result, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: "Generate a list of 4 todo items for project planning",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Track progress
	var totalElements int

	// Create element stream with callbacks
	elementCh := ai.ElementStream[TodoItem](result, ai.ElementStreamOptions[TodoItem]{
		ElementSchema: todoSchema,
		OnElement: func(elem ai.ElementStreamResult[TodoItem]) {
			totalElements++
			fmt.Printf("✓ Received element %d: %s\n", elem.Index+1, elem.Element.Title)
		},
		OnError: func(err error) {
			fmt.Printf("⚠ Error during streaming: %v\n", err)
		},
		OnComplete: func() {
			fmt.Printf("\n✓ Streaming complete! Received %d total elements.\n", totalElements)
		},
	})

	// Also process through the channel
	fmt.Println("Streaming with callbacks...")
	for elem := range elementCh {
		// Elements are already handled by OnElement callback
		// You can do additional processing here if needed
		_ = elem
	}
}
