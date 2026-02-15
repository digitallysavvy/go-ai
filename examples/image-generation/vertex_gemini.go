package main

import (
	"context"
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
)

func main() {
	// Get configuration from environment
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	accessToken := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || accessToken == "" {
		fmt.Println("Please set the following environment variables:")
		fmt.Println("  GOOGLE_VERTEX_PROJECT")
		fmt.Println("  GOOGLE_VERTEX_LOCATION (e.g., us-central1)")
		fmt.Println("  GOOGLE_VERTEX_ACCESS_TOKEN")
		os.Exit(1)
	}

	// Create Google Vertex AI provider
	prov, err := googlevertex.New(googlevertex.Config{
		Project:     project,
		Location:    location,
		AccessToken: accessToken,
	})
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	// Create Gemini image model
	model, err := prov.ImageModel("gemini-2.5-flash-image")
	if err != nil {
		fmt.Printf("Error creating model: %v\n", err)
		os.Exit(1)
	}

	// Generate image with portrait aspect ratio
	fmt.Println("Generating image with Vertex AI Gemini 2.5 Flash...")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A whimsical forest scene with magical creatures and glowing mushrooms",
		Size:   "1080x1920", // Portrait 9:16 aspect ratio
	})
	if err != nil {
		fmt.Printf("Error generating image: %v\n", err)
		os.Exit(1)
	}

	// Save image to file
	filename := "vertex_gemini_output.png"
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
