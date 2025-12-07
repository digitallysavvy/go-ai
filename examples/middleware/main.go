package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/registry"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create provider
	openaiProvider := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Register provider
	registry.RegisterProvider("openai", openaiProvider)

	fmt.Println("=== Middleware Examples ===\n")

	// Example 1: Default settings middleware
	fmt.Println("1. Using default settings middleware:")
	if err := exampleDefaultSettings(openaiProvider); err != nil {
		log.Printf("Error in example 1: %v\n", err)
	}
	fmt.Println()

	// Example 2: Custom middleware
	fmt.Println("2. Using custom middleware:")
	if err := exampleCustomMiddleware(openaiProvider); err != nil {
		log.Printf("Error in example 2: %v\n", err)
	}
	fmt.Println()

	// Example 3: Provider-level middleware
	fmt.Println("3. Using provider-level middleware:")
	if err := exampleProviderMiddleware(openaiProvider); err != nil {
		log.Printf("Error in example 3: %v\n", err)
	}
}

// exampleDefaultSettings demonstrates using default settings middleware
func exampleDefaultSettings(p provider.Provider) error {
	// Get the model
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		return fmt.Errorf("failed to get model: %w", err)
	}

	// Create default settings middleware
	// This will apply temperature=0.3 to all calls unless overridden
	temp := 0.3
	defaultSettings := middleware.DefaultSettingsMiddleware(&provider.GenerateOptions{
		Temperature: &temp,
	})

	// Wrap the model with middleware
	wrappedModel := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{defaultSettings},
		nil, nil,
	)

	// Generate text - will use temperature=0.3 by default
	result, err := ai.Generate(context.Background(), ai.GenerateOptions{
		Model: wrappedModel,
		Messages: []types.Message{
			{Role: "user", Content: types.NewTextContent("Say 'Hello, middleware!'")},
		},
	})
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Printf("Response: %s\n", result.Text)
	return nil
}

// exampleCustomMiddleware demonstrates creating custom middleware
func exampleCustomMiddleware(p provider.Provider) error {
	// Get the model
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		return fmt.Errorf("failed to get model: %w", err)
	}

	// Create custom middleware that adds a system message
	customMiddleware := &middleware.LanguageModelMiddleware{
		SpecificationVersion: "v3",
		TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
			// Add a system message at the beginning
			systemMsg := types.Message{
				Role:    "system",
				Content: types.NewTextContent("You are a helpful assistant. Always respond concisely."),
			}

			// Create new params with system message prepended
			newParams := *params
			newParams.Messages = append([]types.Message{systemMsg}, params.Messages...)

			return &newParams, nil
		},
	}

	// Wrap the model with custom middleware
	wrappedModel := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{customMiddleware},
		nil, nil,
	)

	// Generate text - will have system message automatically added
	result, err := ai.Generate(context.Background(), ai.GenerateOptions{
		Model: wrappedModel,
		Messages: []types.Message{
			{Role: "user", Content: types.NewTextContent("What is 2+2?")},
		},
	})
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Printf("Response: %s\n", result.Text)
	return nil
}

// exampleProviderMiddleware demonstrates applying middleware at provider level
func exampleProviderMiddleware(p provider.Provider) error {
	// Create middleware for all language models from this provider
	temp := 0.5
	languageMiddleware := middleware.DefaultSettingsMiddleware(&provider.GenerateOptions{
		Temperature: &temp,
	})

	// Wrap the entire provider
	wrappedProvider := middleware.WrapProvider(
		p,
		[]*middleware.LanguageModelMiddleware{languageMiddleware},
		nil, // No embedding middleware
	)

	// Now all language models from this provider will have the middleware applied
	model, err := wrappedProvider.LanguageModel("gpt-4o-mini")
	if err != nil {
		return fmt.Errorf("failed to get model: %w", err)
	}

	// Generate text - will use temperature=0.5 by default
	result, err := ai.Generate(context.Background(), ai.GenerateOptions{
		Model: model,
		Messages: []types.Message{
			{Role: "user", Content: types.NewTextContent("Write a haiku about coding.")},
		},
	})
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Printf("Response: %s\n", result.Text)
	return nil
}
