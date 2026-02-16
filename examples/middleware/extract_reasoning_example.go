//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	// Create Anthropic provider
	anthropicProvider := anthropic.New(anthropic.Config{
		APIKey: "your-api-key-here",
	})

	// Get a language model
	model := anthropicProvider.LanguageModel("claude-3-opus-20240229")

	// Apply extractReasoning middleware for Anthropic thinking blocks
	wrappedModel := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{
			middleware.ExtractReasoningMiddleware(&middleware.ExtractReasoningOptions{
				TagName:   "think", // Anthropic uses <think> tags
				Separator: "\n",
				StartWithReasoning: false,
			}),
		},
		nil,
		nil,
	)

	// Example 1: Non-streaming with reasoning extraction
	result, err := wrappedModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: provider.Prompt{
			Text: "Solve this math problem: What is 15 * 24?",
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", result.Text)

	// Example 2: Streaming with reasoning extraction
	stream, err := wrappedModel.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: provider.Prompt{
			Text: "Explain how photosynthesis works",
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n=== Streaming with reasoning extraction ===")
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
			fmt.Printf("[REASONING] %s", chunk.Reasoning)
		case provider.ChunkTypeText:
			fmt.Printf("[TEXT] %s", chunk.Text)
		case provider.ChunkTypeFinish:
			fmt.Printf("\n[FINISH] Reason: %s\n", chunk.FinishReason)
		}
	}

	// Example with OpenAI o1 models (use "reasoning" tag)
	openaiReasoningMiddleware := middleware.ExtractReasoningMiddleware(&middleware.ExtractReasoningOptions{
		TagName:            "reasoning", // OpenAI o1 uses <reasoning> tags
		Separator:          "\n\n",
		StartWithReasoning: true, // OpenAI o1 starts with reasoning
	})

	_ = openaiReasoningMiddleware // Use with OpenAI o1 models
}
