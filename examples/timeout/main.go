package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
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
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: Total timeout
	fmt.Println("=== Example 1: Total Timeout ===")
	totalTimeoutExample(ctx, model)

	// Example 2: Per-step timeout
	fmt.Println("\n=== Example 2: Per-Step Timeout ===")
	perStepTimeoutExample(ctx, model)

	// Example 3: Per-chunk timeout for streaming
	fmt.Println("\n=== Example 3: Per-Chunk Timeout (Streaming) ===")
	perChunkTimeoutExample(ctx, model)

	// Example 4: Combined timeouts
	fmt.Println("\n=== Example 4: Combined Timeouts ===")
	combinedTimeoutExample(ctx, model)

	// Example 5: Builder pattern
	fmt.Println("\n=== Example 5: Builder Pattern ===")
	builderPatternExample(ctx, model)
}

// totalTimeoutExample demonstrates using a total timeout
func totalTimeoutExample(ctx context.Context, model ai.LanguageModel) {
	total := 30 * time.Second
	timeout := &ai.TimeoutConfig{
		Total: &total,
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:   model,
		Prompt:  "Explain quantum computing in detail",
		Timeout: timeout,
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
}

// perStepTimeoutExample demonstrates using per-step timeout for multi-step operations
func perStepTimeoutExample(ctx context.Context, model ai.LanguageModel) {
	perStep := 10 * time.Second
	timeout := &ai.TimeoutConfig{
		PerStep: &perStep,
	}

	// This would be more relevant with tool calling where each step has a timeout
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:   model,
		Prompt:  "What is 2+2?",
		Timeout: timeout,
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
}

// perChunkTimeoutExample demonstrates per-chunk timeout for streaming
func perChunkTimeoutExample(ctx context.Context, model ai.LanguageModel) {
	perChunk := 5 * time.Second
	timeout := &ai.TimeoutConfig{
		PerChunk: &perChunk,
	}

	result, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:   model,
		Prompt:  "Count from 1 to 10",
		Timeout: timeout,
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Read the stream
	text, err := result.ReadAll()
	if err != nil {
		fmt.Printf("Error reading stream: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", text)
}

// combinedTimeoutExample demonstrates using multiple timeout types together
func combinedTimeoutExample(ctx context.Context, model ai.LanguageModel) {
	total := 30 * time.Second
	perStep := 10 * time.Second
	perChunk := 5 * time.Second

	timeout := &ai.TimeoutConfig{
		Total:    &total,
		PerStep:  &perStep,
		PerChunk: &perChunk,
	}

	result, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:   model,
		Prompt:  "Write a haiku about programming",
		Timeout: timeout,
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Process stream chunks
	chunks := result.Chunks()
	for chunk := range chunks {
		if chunk.Type == ai.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}
	fmt.Println()
}

// builderPatternExample demonstrates using the builder pattern for timeout config
func builderPatternExample(ctx context.Context, model ai.LanguageModel) {
	// Use builder pattern to create timeout config
	timeout := (&ai.TimeoutConfig{}).
		WithTotal(30 * time.Second).
		WithPerStep(10 * time.Second).
		WithPerChunk(5 * time.Second)

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:   model,
		Prompt:  "Hello, world!",
		Timeout: timeout,
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Timeout config had total: %v, per-step: %v, per-chunk: %v\n",
		timeout.HasTotal(), timeout.HasPerStep(), timeout.HasPerChunk())
}
