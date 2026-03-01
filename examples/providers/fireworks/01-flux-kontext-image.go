//go:build ignore

// Example: Fireworks AI flux-kontext async image generation
//
// flux-kontext-* models support image generation and editing via an async
// submission-and-polling API. Submit a request, receive a request_id, then
// poll until the image is ready.
//
// Run with:
//
//	FIREWORKS_API_KEY=fw_... go run 01-flux-kontext-image.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
)

func main() {
	apiKey := os.Getenv("FIREWORKS_API_KEY")
	if apiKey == "" {
		log.Fatal("FIREWORKS_API_KEY environment variable is required")
	}

	prov := fireworks.New(fireworks.Config{
		APIKey: apiKey,
	})

	// flux-kontext-dev: high-quality image generation with async polling
	model, err := prov.ImageModel("accounts/fireworks/models/flux-kontext-dev")
	if err != nil {
		log.Fatalf("failed to get model: %v", err)
	}

	fmt.Println("Submitting image generation request...")
	fmt.Println("(flux-kontext models use async polling — this may take a moment)")

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "A serene mountain lake at golden hour, photorealistic, 8k detail",
		AspectRatio: "16:9",
	})
	if err != nil {
		log.Fatalf("image generation failed: %v", err)
	}

	fmt.Println("\nImage generated successfully!")
	if result.URL != "" {
		fmt.Printf("Image URL:  %s\n", result.URL)
	}
	if len(result.Image) > 0 {
		fmt.Printf("Image data: %d bytes (%s)\n", len(result.Image), result.MimeType)
	}

	// Example: flux-kontext-pro for image editing
	fmt.Println("\n--- Image Editing Example ---")
	fmt.Println("(Requires an input image file)")

	editModel, err := prov.ImageModel("accounts/fireworks/models/flux-kontext-pro")
	if err != nil {
		log.Fatalf("failed to get edit model: %v", err)
	}

	// To edit an existing image, provide it via Files:
	//
	//   imageBytes, _ := os.ReadFile("input.png")
	//   editResult, err := editModel.DoGenerate(ctx, &provider.ImageGenerateOptions{
	//       Prompt: "Change the sky to a dramatic sunset with purple clouds",
	//       Files: []provider.ImageFile{
	//           {Type: "file", Data: imageBytes, MediaType: "image/png"},
	//       },
	//   })
	//
	// Or use a URL:
	//
	//   editResult, err := editModel.DoGenerate(ctx, &provider.ImageGenerateOptions{
	//       Prompt: "Add a red barn in the background",
	//       Files: []provider.ImageFile{
	//           {Type: "url", URL: "https://example.com/landscape.png"},
	//       },
	//   })
	_ = editModel
	fmt.Println("Edit model ready:", editModel.ModelID())
	fmt.Println("Set Files in ImageGenerateOptions to use image editing.")
}
