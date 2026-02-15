package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

// Example 4: Text-to-Video Generation
// This example demonstrates generating videos from text prompts using Wan models

func main() {
	// Create Alibaba provider
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Get video model (wan2.6-t2v is the standard text-to-video model)
	model, err := prov.VideoModel("wan2.6-t2v")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Generate a video from text prompt
	fmt.Println("Generating video: 'A golden retriever playing in a park'")
	fmt.Println("This may take 1-2 minutes...")
	fmt.Println()

	duration := 5.0 // 5 seconds
	result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A golden retriever playing in a park, sunny day, slow motion",
		AspectRatio: "16:9",
		Duration:    &duration,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print result
	if len(result.Videos) > 0 {
		video := result.Videos[0]
		fmt.Println("✓ Video generated successfully!")
		fmt.Printf("  URL: %s\n", video.URL)
		fmt.Printf("  Type: %s\n", video.MediaType)
		fmt.Println()

		if result.ProviderMetadata != nil {
			if taskID, ok := result.ProviderMetadata["taskId"].(string); ok {
				fmt.Printf("  Task ID: %s\n", taskID)
			}
		}
	}
}
