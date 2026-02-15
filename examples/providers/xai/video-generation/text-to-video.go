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

	// Generate video with custom settings
	duration := 5.0
	fmt.Println("Generating video from text prompt...")
	fmt.Println("Prompt: A sunset over the ocean with waves crashing")
	fmt.Println("This may take a few minutes as the video is being generated...")

	resp, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A sunset over the ocean with waves crashing",
		Duration:    &duration,
		AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"resolution":     "720p",
				"pollIntervalMs": 5000,  // Check every 5 seconds
				"pollTimeoutMs":  600000, // 10 minute timeout
			},
		},
	})

	if err != nil {
		log.Fatalf("Failed to generate video: %v", err)
	}

	// Print results
	fmt.Println("\n✅ Video generated successfully!")
	fmt.Printf("Video URL: %s\n", resp.Videos[0].URL)
	fmt.Printf("Media Type: %s\n", resp.Videos[0].MediaType)

	// Print metadata
	if metadata, ok := resp.ProviderMetadata["xai"].(map[string]interface{}); ok {
		fmt.Println("\nMetadata:")
		fmt.Printf("  Request ID: %v\n", metadata["requestId"])
		if duration, ok := metadata["duration"]; ok {
			fmt.Printf("  Duration: %.2fs\n", duration)
		}
	}

	// Print warnings
	if len(resp.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range resp.Warnings {
			fmt.Printf("  - %s: %s\n", warning.Type, warning.Message)
		}
	}
}
