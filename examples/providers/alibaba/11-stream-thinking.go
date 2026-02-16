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

// Example 11: Streaming with Thinking Mode (Reasoning)
// This example demonstrates streaming text generation with Alibaba's thinking mode enabled.
// Thinking mode makes the model's reasoning process visible through reasoning_content chunks.

func main() {
	// Create Alibaba provider with API key from environment
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Get qwen-qwq model which supports thinking mode
	model, err := prov.LanguageModel("qwen-qwq-32b-preview")
	if err != nil {
		log.Fatal(err)
	}

	// Create a complex problem that benefits from reasoning
	prompt := types.Prompt{
		Text: "A train travels from City A to City B at 60 mph. Another train travels from City B to City A at 90 mph. The cities are 300 miles apart. When will they meet, and how far from City A?",
	}

	// Configure with thinking mode enabled
	ctx := context.Background()
	stream, err := model.DoStream(ctx, &provider.GenerateOptions{
		Prompt: prompt,
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"enable_thinking":  true,
				"thinking_budget": 5000, // Max tokens for reasoning
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	// Process stream chunks
	fmt.Println("Question:")
	fmt.Println(prompt.Text)
	fmt.Println()

	var usage *types.Usage
	var finishReason types.FinishReason
	var hasReasoning bool

	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		switch chunk.Type {
		case provider.ChunkTypeReasoning:
			// Display reasoning/thinking process
			if !hasReasoning {
				fmt.Println("🤔 Reasoning Process:")
				fmt.Println("---")
				hasReasoning = true
			}
			fmt.Print(chunk.Reasoning)

		case provider.ChunkTypeText:
			// After reasoning, the model provides its answer
			if hasReasoning {
				fmt.Println("\n---")
				fmt.Println()
				fmt.Println("📝 Answer:")
				hasReasoning = false // Reset flag
			}
			fmt.Print(chunk.Text)

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

		// Show reasoning tokens if available
		if usage.OutputDetails != nil && usage.OutputDetails.ReasoningTokens != nil {
			fmt.Printf("  Reasoning: %d tokens\n", *usage.OutputDetails.ReasoningTokens)
		}

		fmt.Printf("  Total:  %d tokens\n", usage.GetTotalTokens())
	}
}
