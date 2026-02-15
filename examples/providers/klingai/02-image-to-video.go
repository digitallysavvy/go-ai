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

	// Get image-to-video model
	model, err := prov.VideoModel("kling-v2.6-i2v")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Generate video from image with start and end frames
	ctx := context.Background()
	duration := 5.0
	startImageURL := "https://example.com/start-frame.png"
	endImageURL := "https://example.com/end-frame.png"

	fmt.Println("Generating video from image with start/end frame control...")
	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "Smoothly transition from the start scene to the end scene",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  startImageURL,
		},
		Duration: &duration,
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				"mode":      "pro", // Pro mode required for start/end frame control
				"imageTail": endImageURL,
				"sound":     "on", // Enable audio generation (V2.6+ pro only)
			},
		},
	})
	if err != nil {
		log.Fatalf("Video generation failed: %v", err)
	}

	// Display results
	fmt.Println("\nVideo generated successfully!")
	fmt.Printf("Video URL: %s\n", response.Videos[0].URL)

	// Display metadata including watermark URL if available
	if metadata, ok := response.ProviderMetadata["klingai"].(map[string]interface{}); ok {
		if videos, ok := metadata["videos"].([]map[string]interface{}); ok && len(videos) > 0 {
			if watermarkURL, ok := videos[0]["watermarkUrl"].(string); ok && watermarkURL != "" {
				fmt.Printf("Watermark Video URL: %s\n", watermarkURL)
			}
			if duration, ok := videos[0]["duration"].(string); ok && duration != "" {
				fmt.Printf("Duration: %s seconds\n", duration)
			}
		}
	}
}
