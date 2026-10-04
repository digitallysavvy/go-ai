// Recipe: stream text as it arrives.
//
// Run: OPENAI_API_KEY=... go run ./examples/recipes/stream-text
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	model, err := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}).
		LanguageModel(openai.ModelGPT6Astra)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model:  model,
		Prompt: "Write a haiku about concurrency.",
	})
	if err != nil {
		log.Fatal(err)
	}

	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
}
