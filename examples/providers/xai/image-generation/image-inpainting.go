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

	// Get source image and mask URLs from environment
	imageURL := os.Getenv("SOURCE_IMAGE_URL")
	if imageURL == "" {
		log.Fatal("SOURCE_IMAGE_URL environment variable is required")
	}

	maskURL := os.Getenv("MASK_IMAGE_URL")
	if maskURL == "" {
		log.Fatal("MASK_IMAGE_URL environment variable is required (white pixels indicate areas to modify)")
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

	// Perform inpainting
	fmt.Println("Performing image inpainting...")
	fmt.Printf("Source Image: %s\n", imageURL)
	fmt.Printf("Mask Image: %s\n", maskURL)
	fmt.Println("Prompt: Add a beautiful rainbow in the masked area")

	result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt: "Add a beautiful rainbow in the masked area",
		Files: []provider.ImageFile{
			{
				Type: "url",
				URL:  imageURL,
			},
		},
		Mask: &provider.ImageFile{
			Type: "url",
			URL:  maskURL,
		},
	})

	if err != nil {
		log.Fatalf("Failed to inpaint image: %v", err)
	}

	// Print results
	fmt.Println("\n✅ Image inpainting completed successfully!")
	fmt.Printf("Image size: %d bytes\n", len(result.Image))
	fmt.Printf("MIME type: %s\n", result.MimeType)

	// Save inpainted image
	outputFile := "inpainted-image.png"
	if err := os.WriteFile(outputFile, result.Image, 0644); err != nil {
		log.Fatalf("Failed to save image: %v", err)
	}

	fmt.Printf("\n💾 Inpainted image saved to: %s\n", outputFile)

	fmt.Println("\n💡 Mask creation tips:")
	fmt.Println("  - White pixels (255,255,255) indicate areas to modify")
	fmt.Println("  - Black pixels (0,0,0) indicate areas to preserve")
	fmt.Println("  - Mask should be same dimensions as source image")
}
