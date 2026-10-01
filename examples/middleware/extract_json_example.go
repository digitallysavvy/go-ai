//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI provider
	openaiProvider := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Get a language model
	model, err := openaiProvider.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	// Apply extractJson middleware
	// This automatically strips markdown code fences from JSON responses
	wrappedModel := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{
			middleware.ExtractJSONMiddleware(nil), // Use default transform
		},
		nil,
		nil,
	)

	// Use the wrapped model
	// If the model responds with: ```json\n{"name": "John"}\n```
	// The middleware will extract: {"name": "John"}
	result, err := wrappedModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Generate a JSON object with a name field",
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Extracted JSON: %s\n", result.Text)

	// Example with custom transform
	customMiddleware := middleware.ExtractJSONMiddleware(&middleware.ExtractJSONOptions{
		Transform: func(text string) string {
			// Custom extraction logic
			// For example, remove a custom prefix
			return text // Add your custom logic here
		},
	})

	wrappedModel2 := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{customMiddleware},
		nil,
		nil,
	)

	result2, err := wrappedModel2.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Generate another JSON object",
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Custom extracted: %s\n", result2.Text)
}
