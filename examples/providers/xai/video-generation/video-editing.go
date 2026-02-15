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

	// Get source video URL from environment
	videoURL := os.Getenv("SOURCE_VIDEO_URL")
	if videoURL == "" {
		log.Fatal("SOURCE_VIDEO_URL environment variable is required (URL of video to edit)")
	}

	// Create XAI provider
	prov := xai.New(xai.Config{
		APIKey: apiKey,
	})

	// Get video model
	model, err := prov.VideoModel("grok-imagine-video")
	if err != nil {
		log.Fatalf("Failed to get video model: %v", err)
	}

	ctx := context.Background()

	// Edit video
	fmt.Println("Editing video...")
	fmt.Printf("Source Video: %s\n", videoURL)
	fmt.Println("Prompt: Add dramatic lighting and apply slow motion effect")
	fmt.Println("This may take a few minutes as the video is being edited...")

	resp, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "Add dramatic lighting and apply slow motion effect",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"videoUrl":       videoURL,
				"pollIntervalMs": 5000,
				"pollTimeoutMs":  600000,
			},
		},
	})

	if err != nil {
		log.Fatalf("Failed to edit video: %v", err)
	}

	// Print results
	fmt.Println("\n✅ Video edited successfully!")
	fmt.Printf("Edited Video URL: %s\n", resp.Videos[0].URL)
	fmt.Printf("Media Type: %s\n", resp.Videos[0].MediaType)

	// Print metadata
	if metadata, ok := resp.ProviderMetadata["xai"].(map[string]interface{}); ok {
		fmt.Println("\nMetadata:")
		fmt.Printf("  Request ID: %v\n", metadata["requestId"])
		if duration, ok := metadata["duration"]; ok {
			fmt.Printf("  Duration: %.2fs\n", duration)
		}
	}

	// Print warnings (video editing doesn't support some options)
	if len(resp.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range resp.Warnings {
			fmt.Printf("  - %s: %s\n", warning.Type, warning.Message)
		}
	}
}
