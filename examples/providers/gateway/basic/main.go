package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
)

func main() {
	// Create gateway provider
	// API key can be set via AI_GATEWAY_API_KEY environment variable
	// or passed directly in the config
	provider, err := gateway.New(gateway.Config{
		APIKey: os.Getenv("AI_GATEWAY_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create gateway provider: %v", err)
	}

	// Get available models
	fmt.Println("Fetching available models...")
	metadata, err := provider.GetAvailableModels(context.Background())
	if err != nil {
		log.Fatalf("Failed to get available models: %v", err)
	}

	fmt.Println("\nAvailable Providers and Models:")
	for _, prov := range metadata.Providers {
		fmt.Printf("\n%s:\n", prov.Name)
		for _, model := range prov.Models {
			fmt.Printf("  - %s (%s)\n", model.Name, model.ID)
		}
	}

	// Check credits
	fmt.Println("\nChecking credits...")
	credits, err := provider.GetCredits(context.Background())
	if err != nil {
		log.Printf("Warning: Failed to get credits: %v", err)
	} else {
		fmt.Printf("Available Credits: %d\n", credits.Available)
		fmt.Printf("Used Credits: %d\n", credits.Used)
	}

	// Create a language model
	// Use OpenAI GPT-4 through the gateway
	model, err := provider.LanguageModel("openai/gpt-4")
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Generating text with gateway provider...")
	fmt.Println(strings.Repeat("=", 60))

	// Generate text
	result, err := ai.GenerateText(context.Background(), model, ai.GenerateTextOptions{
		Prompt: "Explain what the AI Gateway is in one paragraph.",
		MaxTokens: ptr(200),
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("\nResponse:")
	fmt.Println(result.Text)

	// Display usage information
	if result.Usage != nil {
		fmt.Printf("\nUsage:\n")
		fmt.Printf("  Prompt tokens: %d\n", result.Usage.PromptTokens)
		fmt.Printf("  Completion tokens: %d\n", result.Usage.CompletionTokens)
		fmt.Printf("  Total tokens: %d\n", result.Usage.TotalTokens)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Streaming text with gateway provider...")
	fmt.Println(strings.Repeat("=", 60))

	// Stream text
	stream, err := ai.StreamText(context.Background(), model, ai.StreamTextOptions{
		Prompt: "Count from 1 to 5, one number per line.",
	})
	if err != nil {
		log.Fatalf("Failed to stream text: %v", err)
	}

	fmt.Println("\nStreamed response:")
	for chunk := range stream.TextStream {
		fmt.Print(chunk)
	}
	fmt.Println()

	// Check for errors
	if err := stream.Err(); err != nil {
		log.Fatalf("Stream error: %v", err)
	}

	fmt.Println("\nDone!")
}

func ptr[T any](v T) *T {
	return &v
}
