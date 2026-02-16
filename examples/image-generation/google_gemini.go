//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if apiKey == "" {
		fmt.Println("Please set GOOGLE_GENERATIVE_AI_API_KEY environment variable")
		os.Exit(1)
	}

	// Create Google provider
	prov := google.New(google.Config{
		APIKey: apiKey,
	})

	// Create Gemini image model
	model, err := prov.ImageModel("gemini-2.5-flash-image")
	if err != nil {
		fmt.Printf("Error creating model: %v\n", err)
		os.Exit(1)
	}

	// Generate image
	fmt.Println("Generating image with Gemini 2.5 Flash...")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Abstract colorful art with geometric shapes and vibrant patterns",
		Size:   "1024x1024",
	})
	if err != nil {
		fmt.Printf("Error generating image: %v\n", err)
		os.Exit(1)
	}

	// Save image to file
	filename := "gemini_output.png"
	err = os.WriteFile(filename, result.Image, 0644)
	if err != nil {
		fmt.Printf("Error saving image: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Image generated successfully!\n")
	fmt.Printf("  File: %s\n", filename)
	fmt.Printf("  MIME Type: %s\n", result.MimeType)
	fmt.Printf("  Size: %d bytes\n", len(result.Image))
}
