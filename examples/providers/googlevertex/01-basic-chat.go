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

// Example 1: Basic Chat with Google Vertex AI
// This example demonstrates simple text generation using Google Vertex AI's Gemini models

func main() {
	// Create Google Vertex AI provider with credentials from environment
	// Required environment variables:
	//   GOOGLE_VERTEX_PROJECT: Your Google Cloud project ID
	//   GOOGLE_VERTEX_LOCATION: Region (e.g., "us-central1")
	//   GOOGLE_VERTEX_ACCESS_TOKEN: OAuth2 access token
	//
	// To get an access token, run:
	//   gcloud auth print-access-token

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

	// Get a language model (gemini-1.5-flash is fast and cost-effective)
	model, err := prov.LanguageModel(googlevertex.ModelGemini15Flash)
	if err != nil {
		log.Fatal(err)
	}

	// Create a simple prompt
	prompt := types.Prompt{
		Text: "Write a haiku about quantum computing.",
	}

	// Generate text
	ctx := context.Background()
	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the result
	fmt.Println("Generated Haiku:")
	fmt.Println(result.Text)
	fmt.Println()

	// Print token usage
	fmt.Printf("Token Usage:\n")
	fmt.Printf("  Input:  %d tokens\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output: %d tokens\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total:  %d tokens\n", result.Usage.GetTotalTokens())

	// Vertex AI provides additional usage details
	if result.Usage.Raw != nil {
		fmt.Printf("\nVertex AI Usage Details:\n")
		if cachedTokens, ok := result.Usage.Raw["cachedContentTokenCount"].(int); ok && cachedTokens > 0 {
			fmt.Printf("  Cached tokens: %d\n", cachedTokens)
		}
	}
}
