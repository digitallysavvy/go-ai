//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

// Example 10: Streaming Text Generation with Qwen
// This example demonstrates streaming text generation using Alibaba's Qwen models.
// The response is streamed token-by-token as it's generated, allowing for real-time display.

func main() {
	// Create Alibaba provider with API key from environment
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Get a language model (qwen-plus is balanced for performance and cost)
	model, err := prov.LanguageModel("qwen-plus")
	if err != nil {
		log.Fatal(err)
	}

	// Create a prompt
	prompt := types.Prompt{
		Text: "Write a short story about a robot learning to paint. Make it inspiring and concise.",
	}

	// Start streaming generation
	ctx := context.Background()
	stream, err := model.DoStream(ctx, &provider.GenerateOptions{
		Prompt: prompt,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	// Process stream chunks
	fmt.Println("Streaming response:")
	fmt.Println()

	var usage *types.Usage
	var finishReason types.FinishReason

	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		switch chunk.Type {
		case provider.ChunkTypeText:
			// Print text chunks as they arrive (no newline)
			fmt.Print(chunk.Text)

		case provider.ChunkTypeReasoning:
			// If the model uses thinking mode, show reasoning
			fmt.Printf("\n[Reasoning: %s]\n", chunk.Reasoning)

		case provider.ChunkTypeToolCall:
			// Handle tool calls if present
			fmt.Printf("\n[Tool Call: %s(%v)]\n", chunk.ToolCall.ToolName, chunk.ToolCall.Arguments)

		case provider.ChunkTypeFinish:
			// Capture finish reason and usage
			finishReason = chunk.FinishReason
			usage = chunk.Usage
		}
	}

	// Print summary
	fmt.Println("\n")
	fmt.Println("---")
	fmt.Printf("Finish Reason: %s\n", finishReason)

	if usage != nil {
		fmt.Printf("Token Usage:\n")
		fmt.Printf("  Input:  %d tokens\n", usage.GetInputTokens())
		fmt.Printf("  Output: %d tokens\n", usage.GetOutputTokens())
		fmt.Printf("  Total:  %d tokens\n", usage.GetTotalTokens())
	}
}
