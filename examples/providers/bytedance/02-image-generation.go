//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
)

func main() {
	// Create the ByteDance provider.
	// API key is read from BYTEDANCE_API_KEY environment variable.
	prov, err := bytedance.New(bytedance.Config{
		APIKey: os.Getenv("BYTEDANCE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Get the Seedream 5.0 image model
	model, err := prov.ImageModel(string(bytedance.ModelSeedream50))
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	ctx := context.Background()

	fmt.Println("Generating image from text prompt...")

	response, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt: "A cherry blossom tree in a Japanese garden at sunrise, photorealistic",
		Size:   "2048x2048",
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"watermark": false,
			},
		},
	})
	if err != nil {
		log.Fatalf("Image generation failed: %v", err)
	}

	fmt.Println("\nImage generated successfully!")
	fmt.Printf("Images returned: %d\n", len(response.Base64Images))
	fmt.Printf("MIME type hint:  %s\n", response.MimeType)

	if response.Usage.OutputTokens > 0 {
		fmt.Printf("Output tokens: %d\n", response.Usage.OutputTokens)
	}

	// Display any warnings (e.g. unsupported aspectRatio/seed/mask options)
	if len(response.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range response.Warnings {
			fmt.Printf("  [%s] %s\n", w.Type, w.Details)
		}
	}
}
