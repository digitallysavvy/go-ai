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
	prov, err := klingai.New(klingai.Config{})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Get motion control model
	model, err := prov.VideoModel("kling-v2.6-motion-control")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Generate video with reference motion
	ctx := context.Background()
	characterImageURL := "https://example.com/character.png"
	referenceVideoURL := "https://example.com/dance-reference.mp4"

	fmt.Println("Generating video with motion control from reference video...")
	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "A character performing a smooth, graceful dance move",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  characterImageURL,
		},
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				"videoUrl":             referenceVideoURL,
				"characterOrientation": "image", // "image" or "video"
				"mode":                 "std",
				"keepOriginalSound":    "yes",
				"watermarkEnabled":     true,
			},
		},
	})
	if err != nil {
		log.Fatalf("Video generation failed: %v", err)
	}

	// Display results
	fmt.Println("\nVideo generated successfully!")
	fmt.Printf("Video URL: %s\n", response.Videos[0].URL)

	// Display metadata
	if metadata, ok := response.ProviderMetadata["klingai"].(map[string]interface{}); ok {
		fmt.Printf("\nKlingAI Metadata:\n")
		fmt.Printf("  Task ID: %s\n", metadata["taskId"])

		if videos, ok := metadata["videos"].([]map[string]interface{}); ok && len(videos) > 0 {
			if watermarkURL, ok := videos[0]["watermarkUrl"].(string); ok && watermarkURL != "" {
				fmt.Printf("  Watermark URL: %s\n", watermarkURL)
			}
		}
	}

	// Motion control doesn't support custom duration or aspect ratio
	// Output dimensions match the reference video
	if len(response.Warnings) > 0 {
		fmt.Println("\nNote: Motion control has specific constraints:")
		for _, warning := range response.Warnings {
			fmt.Printf("  - %s\n", warning.Message)
		}
	}
}
