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

	// Generate video with motion control in pro mode
	// Pro mode provides higher quality output and supports longer reference videos (up to 30s)
	ctx := context.Background()
	characterImageURL := "https://raw.githubusercontent.com/vercel/ai/refs/heads/main/examples/ai-functions/data/comic-cat.png"
	referenceVideoURL := "https://example.com/dance-reference.mp4"

	fmt.Println("Generating video with motion control (pro mode)...")
	fmt.Printf("Character image: %s\n", characterImageURL)
	fmt.Printf("Reference motion: %s\n", referenceVideoURL)

	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "The character performs a smooth dance move",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  characterImageURL,
		},
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				// Required: URL to the reference motion video
				"videoUrl": referenceVideoURL,
				// Match orientation from the reference video (allows up to 30s)
				"characterOrientation": "video",
				// Pro mode: higher quality output
				"mode": "pro",
				// Keep original audio from the reference video
				"keepOriginalSound": "yes",
				// Enable watermark
				"watermarkEnabled": true,
				// Custom polling settings (pro mode takes longer)
				"pollIntervalMs": 10000,  // 10 seconds
				"pollTimeoutMs":  600000, // 10 minutes
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

	// Display detailed metadata
	if metadata, ok := response.ProviderMetadata["klingai"].(map[string]interface{}); ok {
		fmt.Printf("\nKlingAI Metadata:\n")
		if taskID, ok := metadata["taskId"].(string); ok {
			fmt.Printf("  Task ID: %s\n", taskID)
		}

		if videos, ok := metadata["videos"].([]map[string]interface{}); ok && len(videos) > 0 {
			video := videos[0]
			if watermarkURL, ok := video["watermarkUrl"].(string); ok && watermarkURL != "" {
				fmt.Printf("  Watermark URL: %s\n", watermarkURL)
			}
			if duration, ok := video["duration"].(string); ok && duration != "" {
				fmt.Printf("  Duration: %s seconds\n", duration)
			}
			if width, ok := video["width"].(float64); ok {
				fmt.Printf("  Resolution: %.0fx", width)
				if height, ok := video["height"].(float64); ok {
					fmt.Printf("%.0f\n", height)
				} else {
					fmt.Println()
				}
			}
		}
	}

	// Display warnings if any
	if len(response.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range response.Warnings {
			fmt.Printf("  - %s\n", warning.Message)
		}
	}

	fmt.Println("\nPro Mode Features:")
	fmt.Println("  - Higher quality output")
	fmt.Println("  - Supports reference videos up to 30 seconds")
	fmt.Println("  - Better motion capture accuracy")
	fmt.Println("  - Preserves original audio if requested")
}
