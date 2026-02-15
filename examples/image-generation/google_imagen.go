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

	// Create image model
	model, err := prov.ImageModel("imagen-4.0-generate-001")
	if err != nil {
		fmt.Printf("Error creating model: %v\n", err)
		os.Exit(1)
	}

	// Generate image
	fmt.Println("Generating image with Imagen 4.0...")
	n := 1
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A serene mountain landscape at sunset with dramatic clouds",
		N:      &n,
		Size:   "1024x1024", // Converts to 1:1 aspect ratio
	})
	if err != nil {
		fmt.Printf("Error generating image: %v\n", err)
		os.Exit(1)
	}

	// Save image to file
	filename := "imagen_output.png"
	err = os.WriteFile(filename, result.Image, 0644)
	if err != nil {
		fmt.Printf("Error saving image: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Image generated successfully!\n")
	fmt.Printf("  File: %s\n", filename)
	fmt.Printf("  MIME Type: %s\n", result.MimeType)
	fmt.Printf("  Size: %d bytes\n", len(result.Image))
	fmt.Printf("  Images generated: %d\n", result.Usage.ImageCount)
}
