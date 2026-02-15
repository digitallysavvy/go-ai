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

	// Get source image URL from environment or use default
	imageURL := os.Getenv("SOURCE_IMAGE_URL")
	if imageURL == "" {
		log.Fatal("SOURCE_IMAGE_URL environment variable is required (URL of image to animate)")
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

	// Generate video from image
	fmt.Println("Animating image to video...")
	fmt.Printf("Source Image: %s\n", imageURL)
	fmt.Println("Prompt: Animate this scene with gentle movement, bringing it to life")
	fmt.Println("This may take a few minutes as the video is being generated...")

	resp, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "Animate this scene with gentle movement, bringing it to life",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  imageURL,
		},
		AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"resolution": "720p",
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

	fmt.Println("\n💡 Tip: You can also pass base64-encoded image data instead of URLs")
}
