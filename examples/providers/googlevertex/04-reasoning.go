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

// Example 4: Reasoning/Thinking with Gemini 2.5
// Demonstrates thinking capabilities with Gemini 2.5 models

func main() {
	// Create Google Vertex AI provider
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || token == "" {
		log.Fatal("Missing required environment variables")
	}

	prov, err := googlevertex.New(googlevertex.Config{
		Project:     project,
		Location:    location,
		AccessToken: token,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use Gemini 2.5 for reasoning capabilities
	model, err := prov.LanguageModel("gemini-2.5-flash-preview-04-17")
	if err != nil {
		log.Fatal(err)
	}

	// Stream with reasoning/thinking enabled
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Describe the most unusual or striking architectural feature you've ever seen in a building or structure.",
		},
		// Note: Provider-specific options like thinkingConfig would need to be added
		// to the GenerateOptions struct to fully match the TS SDK
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	// Process chunks, separating reasoning from response
	fmt.Println("Processing stream:")
	for {
		chunk, err := stream.Next()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.Fatal(err)
		}

		switch chunk.Type {
		case provider.ChunkTypeReasoning:
			// Print reasoning in blue (ANSI color code)
			fmt.Print("\033[34m" + chunk.Text + "\033[0m")
		case provider.ChunkTypeText:
			// Print regular text
			fmt.Print(chunk.Text)
		case provider.ChunkTypeFinish:
			fmt.Println()
			fmt.Printf("\nFinish reason: %s\n", chunk.FinishReason)
			if chunk.Usage != nil {
				fmt.Printf("Token usage: %d total\n", chunk.Usage.GetTotalTokens())
				// Check for reasoning tokens
				if chunk.Usage.OutputDetails != nil && chunk.Usage.OutputDetails.ReasoningTokens != nil {
					fmt.Printf("Reasoning tokens: %d\n", *chunk.Usage.OutputDetails.ReasoningTokens)
				}
			}
		}
	}
}
