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
	apiKey := os.Getenv("LUMA_API_KEY")
	if apiKey == "" {
		log.Fatal("LUMA_API_KEY environment variable is required")
	}

	prov := luma.New(luma.Config{APIKey: apiKey})

	model, err := prov.ImageModel(luma.ModelPhotonFlash1)
	if err != nil {
		log.Fatalf("Failed to get image model: %v", err)
	}

	ctx := context.Background()

	fmt.Println("Generating an image styled after a reference image...")

	// Luma only accepts publicly accessible image URLs for reference input
	// (not inline base64/binary data). providerOptions.luma.referenceType
	// selects how the reference is used: "image" (default), "style",
	// "character", or "modify_image". Each entry in providerOptions.luma.images
	// configures the corresponding file in Files, in order.
	result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt: "A cat sitting on a windowsill, painted in this style",
		Files: []provider.ImageFile{
			{Type: "url", URL: "https://example.com/style-reference.jpg"},
		},
		ProviderOptions: map[string]interface{}{
			"luma": map[string]interface{}{
				"referenceType": "style",
				"images": []interface{}{
					map[string]interface{}{"weight": 0.9},
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate image: %v", err)
	}

	fmt.Println("\n✅ Image generated successfully!")
	fmt.Printf("Image size: %d bytes\n", len(result.Image))
	fmt.Printf("MIME type: %s\n", result.MimeType)

	outputFile := "styled-image.png"
	if err := os.WriteFile(outputFile, result.Image, 0644); err != nil {
		log.Fatalf("Failed to save image: %v", err)
	}
	fmt.Printf("\n💾 Image saved to: %s\n", outputFile)
}
