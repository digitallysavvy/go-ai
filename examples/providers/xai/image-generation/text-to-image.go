package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		log.Fatal("XAI_API_KEY environment variable is required")
	}

	// Create XAI provider
	prov := xai.New(xai.Config{
		APIKey: apiKey,
	})

	// Get image model
	model, err := prov.ImageModel("grok-image-1")
	if err != nil {
		log.Fatalf("Failed to get image model: %v", err)
	}

	ctx := context.Background()

	// Generate image
	n := 1
	fmt.Println("Generating image from text prompt...")
	fmt.Println("Prompt: A beautiful sunset over mountains with a lake in the foreground")

	result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "A beautiful sunset over mountains with a lake in the foreground",
		N:           &n,
		AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"output_format": "png",
				"sync_mode":     true,
			},
		},
	})

	if err != nil {
		log.Fatalf("Failed to generate image: %v", err)
	}

	// Print results
	fmt.Println("\n✅ Image generated successfully!")
	fmt.Printf("Image size: %d bytes\n", len(result.Image))
	fmt.Printf("MIME type: %s\n", result.MimeType)
	fmt.Printf("Images generated: %d\n", result.Usage.ImageCount)

	// Save image to file
	outputFile := "generated-image.png"
	if err := os.WriteFile(outputFile, result.Image, 0644); err != nil {
		log.Fatalf("Failed to save image: %v", err)
	}

	fmt.Printf("\n💾 Image saved to: %s\n", outputFile)

	// Print warnings
	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s: %s\n", warning.Type, warning.Message)
		}
	}
}
