package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

// Example 5: Image-to-Video Generation
// This example demonstrates animating a static image using Wan models

func main() {
	// Create Alibaba provider
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Get image-to-video model
	// Use wan2.6-i2v-flash for faster generation with slightly lower quality
	model, err := prov.VideoModel("wan2.6-i2v-flash")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Provide an image URL to animate
	imageURL := "https://example.com/landscape.jpg"

	fmt.Println("Animating image...")
	fmt.Println("Image:", imageURL)
	fmt.Println("This may take 1-2 minutes...")
	fmt.Println()

	duration := 6.0 // 6 seconds
	result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "Add gentle camera movement and natural motion",
		AspectRatio: "16:9",
		Duration:    &duration,
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  imageURL,
		},
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
		fmt.Printf("  Duration: %.1f seconds\n", duration)
	}
}
