//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/luma"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("LUMA_API_KEY")
	if apiKey == "" {
		log.Fatal("LUMA_API_KEY environment variable is required")
	}

	// Create Luma provider
	prov := luma.New(luma.Config{
		APIKey: apiKey,
	})

	// Get the Photon 1 image model
	model, err := prov.ImageModel(luma.ModelPhoton1)
	if err != nil {
		log.Fatalf("Failed to get image model: %v", err)
	}

	ctx := context.Background()

	fmt.Println("Generating image from text prompt...")
	fmt.Println("Prompt: A serene Japanese garden with cherry blossoms at sunrise")

	// Luma generates asynchronously: DoGenerate submits the request, polls
	// the generation's status, and downloads the resulting image once ready.
	result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "A serene Japanese garden with cherry blossoms at sunrise",
		AspectRatio: "16:9",
	})
	if err != nil {
		log.Fatalf("Failed to generate image: %v", err)
	}

	fmt.Println("\n✅ Image generated successfully!")
	fmt.Printf("Image size: %d bytes\n", len(result.Image))
	fmt.Printf("MIME type: %s\n", result.MimeType)
	fmt.Printf("Image URL: %s\n", result.URL)

	// Save image to file
	outputFile := "generated-image.png"
	if err := os.WriteFile(outputFile, result.Image, 0644); err != nil {
		log.Fatalf("Failed to save image: %v", err)
	}
	fmt.Printf("\n💾 Image saved to: %s\n", outputFile)

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s: %s\n", warning.Type, warning.Details)
		}
	}
}
