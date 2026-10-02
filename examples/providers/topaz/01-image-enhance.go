//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/topaz"
)

func main() {
	apiKey := os.Getenv("TOPAZ_API_KEY")
	if apiKey == "" {
		log.Fatal("TOPAZ_API_KEY environment variable is required")
	}

	prov := topaz.New(topaz.Config{APIKey: apiKey})

	model, err := prov.ImageModel(topaz.ImageModelWonder35)
	if err != nil {
		log.Fatalf("Failed to get image model: %v", err)
	}

	ctx := context.Background()

	fmt.Println("Enhancing image...")

	// Topaz enhances an image the caller supplies, so it does not take a
	// text prompt. A url-type file is forwarded to Topaz as source_url
	// (Topaz fetches it itself); a file-type file is uploaded directly.
	// DoGenerate submits the async enhance job, polls its status, and
	// downloads the resulting image, all in this one call.
	result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{
			{Type: "url", URL: "https://example.com/input.png"},
		},
		Size: "4000x3000",
	})
	if err != nil {
		log.Fatalf("Failed to enhance image: %v", err)
	}

	fmt.Println("\nImage enhanced successfully!")
	fmt.Printf("Image size: %d bytes\n", len(result.Image))
	fmt.Printf("MIME type: %s\n", result.MimeType)

	if topazMeta, ok := result.ProviderMetadata["topaz"].(map[string]interface{}); ok {
		if images, ok := topazMeta["images"].([]interface{}); ok && len(images) > 0 {
			fmt.Printf("Topaz metadata: %#v\n", images[0])
		}
	}

	outputFile := "enhanced-image.png"
	if err := os.WriteFile(outputFile, result.Image, 0644); err != nil {
		log.Fatalf("Failed to save image: %v", err)
	}
	fmt.Printf("\nImage saved to: %s\n", outputFile)

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s: %s\n", warning.Type, warning.Details)
		}
	}
}
