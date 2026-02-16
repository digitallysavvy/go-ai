//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
)

// Example 2: Streaming Text with Google Vertex AI
// Demonstrates real-time text streaming using the Next() method

func main() {
	// Create Google Vertex AI provider
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || token == "" {
		log.Fatal("Missing required environment variables:\n" +
			"  GOOGLE_VERTEX_PROJECT\n" +
			"  GOOGLE_VERTEX_LOCATION\n" +
			"  GOOGLE_VERTEX_ACCESS_TOKEN")
	}

	prov, err := googlevertex.New(googlevertex.Config{
		Project:     project,
		Location:    location,
		AccessToken: token,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create language model
	model, err := prov.LanguageModel(googlevertex.ModelGemini15Flash)
	if err != nil {
		log.Fatal(err)
	}

	// Start streaming
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Invent a new holiday and describe its traditions.",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	// Process chunks using Next() method
	fmt.Println("Streaming response:")
	for {
		chunk, err := stream.Next()
		if err != nil {
			// Check for end of stream
			if err.Error() == "EOF" {
				break
			}
			log.Fatal(err)
		}

		switch chunk.Type {
		case provider.ChunkTypeText:
			// Print text as it arrives
			fmt.Print(chunk.Text)
		case provider.ChunkTypeFinish:
			fmt.Println()
			fmt.Printf("\nFinish reason: %s\n", chunk.FinishReason)
			if chunk.Usage != nil {
				fmt.Printf("Token usage: %d total\n", chunk.Usage.GetTotalTokens())
			}
		}
	}
}
