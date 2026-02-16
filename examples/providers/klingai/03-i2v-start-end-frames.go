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
	prov, err := klingai.New(klingai.Config{})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Get image-to-video model
	model, err := prov.VideoModel("kling-v2.6-i2v")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Generate video with start and end frame control
	// This allows you to control both the beginning and ending state of the video
	ctx := context.Background()
	duration := 5.0
	startImageURL := "https://raw.githubusercontent.com/vercel/ai/refs/heads/main/examples/ai-functions/data/comic-cat.png"
	endImageURL := "https://raw.githubusercontent.com/vercel/ai/refs/heads/main/examples/ai-functions/data/comic-dog.png"

	fmt.Println("Generating video with start and end frame control...")
	fmt.Printf("Start frame: %s
", startImageURL)
	fmt.Printf("End frame: %s
", endImageURL)

	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "The cat walks across the scene and transforms into a dog by the end, in a playful and cartoonish style.",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  startImageURL,
		},
		Duration: &duration,
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				"mode":      "pro", // Pro mode required for start/end frame control
				"imageTail": endImageURL, // End frame for precise control
				"sound":     "on", // Enable audio generation (V2.6+ pro only)
				"pollTimeoutMs": 600000, // 10 minutes for pro mode
			},
		},
	})
	if err != nil {
		log.Fatalf("Video generation failed: %v", err)
	}

	// Display results
	fmt.Println("
Video generated successfully!")
	fmt.Printf("Video URL: %s
", response.Videos[0].URL)
	fmt.Printf("Media Type: %s
", response.Videos[0].MediaType)

	// Display metadata including watermark URL if available
	if metadata, ok := response.ProviderMetadata["klingai"].(map[string]interface{}); ok {
		fmt.Printf("
KlingAI Metadata:
")
		if taskID, ok := metadata["taskId"].(string); ok {
			fmt.Printf("  Task ID: %s
", taskID)
		}

		if videos, ok := metadata["videos"].([]map[string]interface{}); ok && len(videos) > 0 {
			if watermarkURL, ok := videos[0]["watermarkUrl"].(string); ok && watermarkURL != "" {
				fmt.Printf("  Watermark Video URL: %s
", watermarkURL)
			}
			if dur, ok := videos[0]["duration"].(string); ok && dur != "" {
				fmt.Printf("  Duration: %s seconds
", dur)
			}
		}
	}

	// Display warnings if any
	if len(response.Warnings) > 0 {
		fmt.Println("
Warnings:")
		for _, warning := range response.Warnings {
			fmt.Printf("  - %s
", warning.Message)
		}
	}
}
