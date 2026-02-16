package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
)

// This example demonstrates basic text generation using a local LLM
// Prerequisites:
// 1. Install LMStudio from https://lmstudio.ai/
// 2. Download a model (e.g., Mistral-7B, Llama-2-7B)
// 3. Start the local server (default: http://localhost:1234)
// 4. Load a model in LMStudio

func main() {
	// Create Open Responses provider pointing to LMStudio
	provider := openresponses.New(openresponses.Config{
		BaseURL: "http://localhost:1234/v1",
		// No API key needed for LMStudio
	})

	// Get language model
	// Use the model ID shown in LMStudio or use "local-model" as default
	model, err := provider.LanguageModel("local-model")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Basic Text Generation with Local LLM ===")
	fmt.Println("Asking: Explain Go interfaces in simple terms")

	// Generate text
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Explain Go interfaces in simple terms",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print response
	fmt.Println("Response:")
	fmt.Println(result.Text)

	// Print usage information
	fmt.Println("\nUsage:")
	fmt.Printf("  Input tokens:  %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total tokens:  %d\n", result.Usage.GetTotalTokens())
	fmt.Printf("  Finish reason: %s\n", result.FinishReason)

	// Example 2: Using with system message
	fmt.Println("\n\n=== With System Message ===")
	fmt.Println("System: You are a helpful coding assistant")
	fmt.Println("User: How do I handle errors in Go?")

	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful coding assistant who gives concise, practical answers.",
		Prompt: "How do I handle errors in Go?",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Response:")
	fmt.Println(result2.Text)

	// Example 3: With generation parameters
	fmt.Println("\n\n=== With Custom Parameters ===")
	fmt.Println("Generating a creative response with higher temperature...")

	temperature := 0.8
	maxTokens := 100

	result3, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:       model,
		Prompt:      "Write a creative tagline for a Go programming conference",
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Response:")
	fmt.Println(result3.Text)
}
