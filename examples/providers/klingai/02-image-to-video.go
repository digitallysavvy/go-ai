//go:build ignore
// +build ignore

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

	// Get image-to-video model
	model, err := prov.VideoModel("kling-v2.6-i2v")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Animate a static image with a slow zoom-in camera movement
	ctx := context.Background()
	duration := 5.0
	imageURL := "https://raw.githubusercontent.com/vercel/ai/refs/heads/main/examples/ai-functions/data/comic-cat.png"
	zoom := 5.0

	fmt.Println("Generating video from a single image...")
	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "The cat looks around curiously as the camera slowly zooms in",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  imageURL,
		},
		AspectRatio: "16:9",
		Duration:    &duration,
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				"mode": "std",
				"cameraControl": map[string]interface{}{
					"type": "simple",
					"config": map[string]interface{}{
						"zoom": zoom,
					},
				},
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
		if taskID, ok := metadata["taskId"].(string); ok {
			fmt.Printf("  Task ID: %s\n", taskID)
		}
	}

	// Display warnings if any
	if len(response.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range response.Warnings {
			fmt.Printf("  - %s\n", warning.Message)
		}
	}
}
