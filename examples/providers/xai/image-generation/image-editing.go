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

	// Get source image URL from environment
	imageURL := os.Getenv("SOURCE_IMAGE_URL")
	if imageURL == "" {
		log.Fatal("SOURCE_IMAGE_URL environment variable is required (URL of image to edit)")
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

	// Edit image
	fmt.Println("Editing image...")
	fmt.Printf("Source Image: %s\n", imageURL)
	fmt.Println("Prompt: Change the sky to vibrant sunset colors with orange and pink hues")

	result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt: "Change the sky to vibrant sunset colors with orange and pink hues",
		Files: []provider.ImageFile{
			{
				Type: "url",
				URL:  imageURL,
			},
		},
	})

	if err != nil {
		log.Fatalf("Failed to edit image: %v", err)
	}

	// Print results
	fmt.Println("\n✅ Image edited successfully!")
	fmt.Printf("Image size: %d bytes\n", len(result.Image))
	fmt.Printf("MIME type: %s\n", result.MimeType)

	// Save edited image
	outputFile := "edited-image.png"
	if err := os.WriteFile(outputFile, result.Image, 0644); err != nil {
		log.Fatalf("Failed to save image: %v", err)
	}

	fmt.Printf("\n💾 Edited image saved to: %s\n", outputFile)

	fmt.Println("\n💡 Tip: You can also pass base64-encoded image data instead of URLs")
}
