package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
)

func main() {
	// Create gateway provider
	// API key can be set via AI_GATEWAY_API_KEY environment variable
	// or passed directly in the config
	provider, err := gateway.New(gateway.Config{
		APIKey: os.Getenv("AI_GATEWAY_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create gateway provider: %v", err)
	}

	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("Gateway Video Generation Example")
	fmt.Println(strings.Repeat("=", 70))

	// Example 1: Generate video with Google Veo
	fmt.Println("\nExample 1: Text-to-Video with Google Veo")
	fmt.Println(strings.Repeat("-", 70))

	// Create video model
	// The gateway will route the request to the best available provider
	videoModel, err := provider.VideoModel("google/veo-3.1-generate-001")
	if err != nil {
		log.Fatalf("Failed to create video model: %v", err)
	}

	// Generate video
	result, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model: videoModel,
		Prompt: ai.VideoPrompt{
			Text: "A resplendent quetzal flying through a misty rainforest canopy at dawn",
		},
		AspectRatio: "16:9",
		Duration:    ptr(4.0), // 4 seconds
		N:           1,
	})
	if err != nil {
		log.Fatalf("Failed to generate video: %v", err)
	}

	fmt.Printf("\n✓ Video generated successfully!\n")
	fmt.Printf("  Number of videos: %d\n", len(result.Videos))
	if len(result.Videos) > 0 {
		video := result.Videos[0]
		fmt.Printf("  Media type: %s\n", video.MediaType)
		if video.URL != "" {
			fmt.Printf("  URL: %s\n", video.URL)
		}
		if len(video.Data) > 0 {
			fmt.Printf("  Data length: %d bytes\n", len(video.Data))
		}
	}

	// Display warnings if any
	if len(result.Warnings) > 0 {
		fmt.Println("\n⚠️  Warnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - [%s] %s", warning.Type, warning.Message)
			if warning.Type != "" {
				fmt.Printf(" (feature: %s)", warning.Type)
			}
			fmt.Println()
		}
	}

	// Example 2: Video generation with provider options
	fmt.Println("\n\nExample 2: Video with Provider Options")
	fmt.Println(strings.Repeat("-", 70))

	result2, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model: videoModel,
		Prompt: ai.VideoPrompt{
			Text: "A cat playing with a ball of yarn in slow motion",
		},
		AspectRatio: "1:1",  // Square video
		Duration:    ptr(3.0), // 3 seconds
		FPS:         ptr(30), // 30 frames per second
		ProviderOptions: map[string]interface{}{
			"enhancePrompt": true,
		},
	})
	if err != nil {
		log.Printf("Warning: Failed to generate second video: %v", err)
	} else {
		fmt.Printf("\n✓ Second video generated successfully!\n")
		fmt.Printf("  Number of videos: %d\n", len(result2.Videos))
	}

	// Example 3: Multiple video generation
	fmt.Println("\n\nExample 3: Batch Video Generation")
	fmt.Println(strings.Repeat("-", 70))

	result3, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model: videoModel,
		Prompt: ai.VideoPrompt{
			Text: "Ocean waves crashing on a rocky shore",
		},
		AspectRatio: "16:9",
		Duration:    ptr(2.0),
		N:           2, // Generate 2 videos
	})
	if err != nil {
		log.Printf("Warning: Failed to generate batch videos: %v", err)
	} else {
		fmt.Printf("\n✓ Batch videos generated successfully!\n")
		fmt.Printf("  Number of videos: %d\n", len(result3.Videos))
		for i, video := range result3.Videos {
			fmt.Printf("  Video %d: %s (%s)\n", i+1, video.MediaType, video.MediaType)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("Video generation completed!")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("\nNote: Video generation can take several minutes depending on the provider.")
	fmt.Println("The gateway automatically selects the best available provider based on:")
	fmt.Println("  - Provider availability")
	fmt.Println("  - Model capabilities")
	fmt.Println("  - Cost optimization")
	fmt.Println("  - Routing rules and tags")
}

func ptr[T any](v T) *T {
	return &v
}
