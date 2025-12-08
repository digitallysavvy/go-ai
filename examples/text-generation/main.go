package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

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
	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: Simple text generation
	fmt.Println("=== Example 1: Simple Text Generation ===")
	simpleExample(ctx, model)

	// Example 2: Streaming text generation
	fmt.Println("\n=== Example 2: Streaming Text Generation ===")
	streamingExample(ctx, model)

	// Example 3: Tool calling
	fmt.Println("\n=== Example 3: Tool Calling ===")
	toolCallingExample(ctx, model)
}

func simpleExample(ctx context.Context, model provider.LanguageModel) {
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Tell me a joke about programming",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Tokens used: %d (input: %d, output: %d)\n",
		result.Usage.TotalTokens,
		result.Usage.InputTokens,
		result.Usage.OutputTokens)
}

func streamingExample(ctx context.Context, model provider.LanguageModel) {
	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: "Write a haiku about Go programming",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Print("Response: ")
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}
	fmt.Println()

	if err := stream.Err(); err != nil {
		log.Printf("Stream error: %v", err)
		return
	}

	usage := stream.Usage()
	fmt.Printf("Tokens used: %d\n", usage.TotalTokens)
}

func toolCallingExample(ctx context.Context, model provider.LanguageModel) {
	// Define a weather tool
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g., San Francisco, CA",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			location := params["location"].(string)
			// Simulate weather API call
			return map[string]interface{}{
				"location":    location,
				"temperature": 72,
				"condition":   "sunny",
			}, nil
		},
	}

	maxSteps := 5
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What's the weather like in San Francisco?",
		Tools:    []types.Tool{weatherTool},
		MaxSteps: &maxSteps,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Final response: %s\n", result.Text)
	fmt.Printf("Steps taken: %d\n", len(result.Steps))
	if len(result.ToolCalls) > 0 {
		fmt.Printf("Tools called: %d\n", len(result.ToolCalls))
	}
}
