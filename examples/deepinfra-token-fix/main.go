package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
)

// This example demonstrates the DeepInfra provider with fixed token counting
// for Gemini/Gemma models.
//
// DeepInfra's API has a bug where it doesn't include reasoning_tokens in
// completion_tokens for Gemini/Gemma models. This Go SDK automatically
// fixes this issue.
//
// Prerequisites:
// 1. Set DEEPINFRA_API_KEY environment variable
// 2. Run: go run main.go

func main() {
	apiKey := os.Getenv("DEEPINFRA_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPINFRA_API_KEY environment variable is required")
	}

	// Create DeepInfra provider
	provider := deepinfra.New(deepinfra.Config{
		APIKey: apiKey,
	})

	fmt.Println("=== DeepInfra Token Counting Fix Demo ===")
	fmt.Println()

	// Example 1: Gemini model with reasoning
	fmt.Println("Example 1: Using Gemini model with reasoning")
	geminiModel, err := provider.LanguageModel("google/gemini-2.0-flash-thinking-exp-1219")
	if err != nil {
		log.Fatalf("Failed to create Gemini model: %v", err)
	}

	result1, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  geminiModel,
		Prompt: "Explain quantum entanglement and why it's significant for quantum computing.",
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n\n", result1.Text)

	// Display token usage
	if result1.Usage.TotalTokens != nil {
		fmt.Println("Token Usage (automatically fixed):")
		if result1.Usage.InputTokens != nil {
			fmt.Printf("  Input tokens:  %d\n", *result1.Usage.InputTokens)
		}
		if result1.Usage.OutputTokens != nil {
			fmt.Printf("  Output tokens: %d\n", *result1.Usage.OutputTokens)
		}

		// Show breakdown if available
		if result1.Usage.OutputDetails != nil {
			if result1.Usage.OutputDetails.TextTokens != nil {
				fmt.Printf("    - Text tokens:      %d\n", *result1.Usage.OutputDetails.TextTokens)
			}
			if result1.Usage.OutputDetails.ReasoningTokens != nil {
				fmt.Printf("    - Reasoning tokens: %d\n", *result1.Usage.OutputDetails.ReasoningTokens)
			}
		}

		fmt.Printf("  Total tokens:  %d\n", *result1.Usage.TotalTokens)
		fmt.Println()

		// Explain the fix
		if result1.Usage.OutputDetails != nil &&
		   result1.Usage.OutputDetails.ReasoningTokens != nil &&
		   *result1.Usage.OutputDetails.ReasoningTokens > 0 {
			fmt.Println("ℹ️  Token counting was automatically corrected:")
			fmt.Println("   DeepInfra's API doesn't include reasoning_tokens in completion_tokens")
			fmt.Println("   for Gemini/Gemma models. This SDK automatically fixes the count.")
			fmt.Println()
		}
	}

	// Example 2: Gemma model
	fmt.Println("Example 2: Using Gemma model")
	gemmaModel, err := provider.LanguageModel("google/gemma-2-9b-it")
	if err != nil {
		log.Fatalf("Failed to create Gemma model: %v", err)
	}

	result2, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  gemmaModel,
		Prompt: "What are the benefits of using Go for AI applications?",
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n\n", result2.Text)

	if result2.Usage.TotalTokens != nil {
		fmt.Println("Token Usage:")
		if result2.Usage.InputTokens != nil {
			fmt.Printf("  Input tokens:  %d\n", *result2.Usage.InputTokens)
		}
		if result2.Usage.OutputTokens != nil {
			fmt.Printf("  Output tokens: %d\n", *result2.Usage.OutputTokens)
		}
		fmt.Printf("  Total tokens:  %d\n", *result2.Usage.TotalTokens)
		fmt.Println()
	}

	// Example 3: Streaming with token fix
	fmt.Println("Example 3: Streaming response with token counting fix")
	stream, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model:  geminiModel,
		Prompt: "Count from 1 to 5 with explanations.",
	})
	if err != nil {
		log.Fatalf("StreamText failed: %v", err)
	}

	fmt.Print("Streaming response: ")
	for {
		chunk, err := stream.Read()
		if err != nil {
			break
		}
		fmt.Print(chunk.Text)
	}
	fmt.Println()

	if stream.Usage().TotalTokens != nil {
		fmt.Println("\nFinal token usage:")
		if stream.Usage().InputTokens != nil {
			fmt.Printf("  Input tokens:  %d\n", *stream.Usage().InputTokens)
		}
		if stream.Usage().OutputTokens != nil {
			fmt.Printf("  Output tokens: %d\n", *stream.Usage().OutputTokens)
		}
		if stream.Usage().OutputDetails != nil && stream.Usage().OutputDetails.ReasoningTokens != nil {
			fmt.Printf("    - Text tokens:      %d\n", *stream.Usage().OutputDetails.TextTokens)
			fmt.Printf("    - Reasoning tokens: %d\n", *stream.Usage().OutputDetails.ReasoningTokens)
		}
		fmt.Printf("  Total tokens:  %d\n", *stream.Usage().TotalTokens)
	}

	fmt.Println("\n✅ Complete! Token counting has been automatically fixed for Gemini/Gemma models.")
}
