//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI provider
	openaiProvider := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Get a language model
	model, err := openaiProvider.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	// Apply simulateStreaming middleware
	// This allows you to use streaming APIs even with providers that don't support streaming
	// or to test streaming behavior with non-streaming responses
	wrappedModel := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{
			middleware.SimulateStreamingMiddleware(),
		},
		nil,
		nil,
	)

	// Now you can use DoStream even if the underlying provider doesn't support it
	// The middleware will call DoGenerate internally and simulate streaming
	stream, err := wrappedModel.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Write a haiku about programming",
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Simulated Streaming ===")
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
			fmt.Printf("%s", chunk.Text)
		case provider.ChunkTypeToolCall:
			fmt.Printf("\n[TOOL CALL] %s: %v\n", chunk.ToolCall.ToolName, chunk.ToolCall.Arguments)
		case provider.ChunkTypeUsage:
			fmt.Printf("\n[USAGE] Total tokens: %d\n", chunk.Usage.GetTotalTokens())
		case provider.ChunkTypeFinish:
			fmt.Printf("\n[FINISH] Reason: %s\n", chunk.FinishReason)
		}
	}

	// This is useful for:
	// 1. Testing streaming behavior in development
	// 2. Supporting providers that don't have native streaming
	// 3. Providing consistent streaming interface across all providers
	// 4. Converting batch responses to streaming for UI compatibility
}
