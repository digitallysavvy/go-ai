//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
)

// This example demonstrates streaming text generation with a local LLM
// Prerequisites:
// 1. Install and start LMStudio (or another service)
// 2. Load a model that supports streaming

func main() {
	// Create Open Responses provider
	prov := openresponses.New(openresponses.Config{
		BaseURL: "http://localhost:1234/v1",
	})

	// Get language model
	model, err := prov.LanguageModel("local-model")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Streaming Text Generation with Local LLM ===
")

	// Example 1: Basic streaming
	fmt.Println("Example 1: Basic Streaming")
	basicStreamingExample(ctx, model)

	fmt.Println("

" + strings.Repeat("=", 50))

	// Example 2: Streaming with token count
	fmt.Println("

Example 2: Streaming with Token Tracking")
	streamingWithTokensExample(ctx, model)

	fmt.Println("

" + strings.Repeat("=", 50))

	// Example 3: Streaming a longer response
	fmt.Println("

Example 3: Streaming Longer Content")
	streamingLongContentExample(ctx, model)
}

func basicStreamingExample(ctx context.Context, model provider.LanguageModel) {
	fmt.Println("Prompt: Explain what Go channels are in one sentence
")
	fmt.Print("Response: ")

	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: "Explain what Go channels are in one sentence",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	// Read and print chunks as they arrive
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}

	// Check for errors
	if err := stream.Err(); err != nil {
		log.Printf("
Stream error: %v", err)
		return
	}

	fmt.Println()
	fmt.Printf("
Finish reason: %s
", stream.FinishReason())
}

func streamingWithTokensExample(ctx context.Context, model provider.LanguageModel) {
	fmt.Println("Prompt: Write a haiku about Go programming
")
	fmt.Print("Response:
")

	startTime := time.Now()

	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: "Write a haiku about Go programming",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	var fullText string
	var chunkCount int

	// Read and print chunks
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
			fullText += chunk.Text
			chunkCount++
		}
	}

	duration := time.Since(startTime)

	// Check for errors
	if err := stream.Err(); err != nil {
		log.Printf("
Stream error: %v", err)
		return
	}

	fmt.Println()

	// Print streaming stats
	usage := stream.Usage()
	fmt.Println("
Streaming Stats:")
	fmt.Printf("  Chunks received: %d
", chunkCount)
	fmt.Printf("  Duration: %v
", duration)
	fmt.Printf("  Total tokens: %d
", usage.GetTotalTokens())
	outputTokens := usage.GetOutputTokens()
	if outputTokens > 0 {
		tokensPerSecond := float64(outputTokens) / duration.Seconds()
		fmt.Printf("  Tokens/second: %.2f
", tokensPerSecond)
	}
}

func streamingLongContentExample(ctx context.Context, model provider.LanguageModel) {
	fmt.Println("Prompt: Write a short story about a robot learning to code
")
	fmt.Println("Response:")
	fmt.Println(strings.Repeat("-", 50))

	maxTokens := 200
	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:     model,
		Prompt:    "Write a short story (3 paragraphs) about a robot learning to code",
		MaxTokens: &maxTokens,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	var fullText string

	// Read and print chunks with visual feedback
	for chunk := range stream.Chunks() {
		switch chunk.Type {
		case provider.ChunkTypeText:
			fmt.Print(chunk.Text)
			fullText += chunk.Text

		case provider.ChunkTypeToolCall:
			// Handle tool calls if model decides to use them
			fmt.Printf("
[Tool call: %s]
", chunk.ToolCall.ToolName)
		}
	}

	// Check for errors
	if err := stream.Err(); err != nil {
		log.Printf("

Stream error: %v", err)
		return
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 50))

	// Print final stats
	usage := stream.Usage()
	fmt.Println("
Final Stats:")
	fmt.Printf("  Characters generated: %d
", len(fullText))
	fmt.Printf("  Input tokens: %d
", usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d
", usage.GetOutputTokens())
	fmt.Printf("  Finish reason: %s
", stream.FinishReason())
}
