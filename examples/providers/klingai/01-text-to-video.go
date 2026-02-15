package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/klingai"
)

func main() {
	// Create KlingAI provider
	// Credentials loaded from KLINGAI_ACCESS_KEY and KLINGAI_SECRET_KEY env vars
	prov, err := klingai.New(klingai.Config{})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Get text-to-video model
	model, err := prov.VideoModel("kling-v2.6-t2v")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Generate video
	ctx := context.Background()
	duration := 5.0

	fmt.Println("Generating video from text prompt...")
	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A chicken flying into the sunset in the style of 90s anime",
		AspectRatio: "16:9",
		Duration:    &duration,
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				"mode":           "std",
				"negativePrompt": "low quality, blurry, distorted",
			},
		},
	})
	if err != nil {
		log.Fatalf("Video generation failed: %v", err)
	}

	// Display results
	fmt.Println("\nVideo generated successfully!")
	fmt.Printf("Video URL: %s\n", response.Videos[0].URL)
	fmt.Printf("Media Type: %s\n", response.Videos[0].MediaType)

	// Display metadata
	if metadata, ok := response.ProviderMetadata["klingai"].(map[string]interface{}); ok {
		fmt.Printf("\nKlingAI Metadata:\n")
		fmt.Printf("  Task ID: %s\n", metadata["taskId"])
	}

	// Display warnings if any
	if len(response.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range response.Warnings {
			fmt.Printf("  - %s\n", warning.Message)
		}
	}
}
