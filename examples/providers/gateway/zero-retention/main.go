package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
)

func main() {
	fmt.Println("=======================================================")
	fmt.Println("AI Gateway - Zero Data Retention Example")
	fmt.Println("=======================================================")
	fmt.Println()
	fmt.Println("This example demonstrates how to use the AI Gateway")
	fmt.Println("with zero data retention mode enabled.")
	fmt.Println()
	fmt.Println("When zero data retention is enabled:")
	fmt.Println("  - Requests are not logged by the gateway")
	fmt.Println("  - No data is retained for analysis or debugging")
	fmt.Println("  - Suitable for sensitive or privacy-critical applications")
	fmt.Println()
	fmt.Println("=======================================================")
	fmt.Println()

	// Create gateway provider with zero data retention enabled
	provider, err := gateway.New(gateway.Config{
		APIKey:            os.Getenv("AI_GATEWAY_API_KEY"),
		ZeroDataRetention: true, // Enable zero data retention
	})
	if err != nil {
		log.Fatalf("Failed to create gateway provider: %v", err)
	}

	fmt.Println("✓ Gateway provider created with zero data retention enabled")

	// Create language model
	model, err := provider.LanguageModel("openai/gpt-4")
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	fmt.Println("✓ Language model initialized")
	fmt.Println()

	// Example 1: Generate text with sensitive information
	fmt.Println("Example 1: Processing sensitive data")
	fmt.Println("---------------------------------------")

	result1, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
		Prompt: "Explain the importance of data privacy in healthcare applications.",
		MaxTokens: ptr(150),
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("\nResponse:")
	fmt.Println(result1.Text)
	fmt.Println()

	// Example 2: Multiple requests with zero retention
	fmt.Println("Example 2: Multiple requests (all with zero retention)")
	fmt.Println("--------------------------------------------------------")

	prompts := []string{
		"What are best practices for secure API design?",
		"How should companies handle user data deletion requests?",
		"What is GDPR and why is it important?",
	}

	for i, prompt := range prompts {
		fmt.Printf("\nRequest %d: %s\n", i+1, prompt)

		result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
			Prompt:    prompt,
			MaxTokens: ptr(100),
		})
		if err != nil {
			log.Printf("Warning: Request %d failed: %v", i+1, err)
			continue
		}

		fmt.Printf("Response: %s\n", truncate(result.Text, 200))
	}

	// Example 3: Streaming with zero retention
	fmt.Println("\n=======================================================")
	fmt.Println("Example 3: Streaming with zero data retention")
	fmt.Println("=======================================================")

	stream, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model: model,
		Prompt: "List 3 key principles of privacy-by-design.",
	})
	if err != nil {
		log.Fatalf("Failed to stream text: %v", err)
	}

	fmt.Println("\nStreaming response:")
	for chunk := range stream.Chunks() {
		fmt.Print(chunk.Text)
	}
	fmt.Println()

	if err := stream.Err(); err != nil {
		log.Fatalf("Stream error: %v", err)
	}

	// Example 4: Compare with normal mode (informational only)
	fmt.Println("\n=======================================================")
	fmt.Println("Comparison: Zero Retention vs Normal Mode")
	fmt.Println("=======================================================")
	fmt.Println()
	fmt.Println("Zero Data Retention Mode (Current):")
	fmt.Println("  ✓ No request logging")
	fmt.Println("  ✓ No data retention")
	fmt.Println("  ✓ Maximum privacy")
	fmt.Println("  ✗ No debugging assistance from gateway")
	fmt.Println()
	fmt.Println("Normal Mode (Default):")
	fmt.Println("  ✓ Request logging for debugging")
	fmt.Println("  ✓ Gateway can assist with issues")
	fmt.Println("  ✗ Data retained for analysis")
	fmt.Println()
	fmt.Println("Choose the mode that best fits your privacy requirements.")
	fmt.Println()

	fmt.Println("Done!")
}

func ptr[T any](v T) *T {
	return &v
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
