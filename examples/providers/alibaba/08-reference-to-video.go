package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

// Example 8: Reference-to-Video Generation
// This example demonstrates generating videos with style transfer from a reference image

func main() {
	// Create Alibaba provider
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Get reference-to-video model
	// Use wan2.6-r2v for quality, or wan2.6-r2v-flash for speed
	model, err := prov.VideoModel("wan2.6-r2v")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Provide a reference image that defines the visual style
	referenceImageURL := "https://example.com/anime-style.jpg"

	fmt.Println("Generating video with reference style transfer...")
	fmt.Printf("Reference image: %s\n", referenceImageURL)
	fmt.Println("This may take 1-2 minutes...")
	fmt.Println()

	duration := 5.0 // 5 seconds
	result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A cat walking through a garden",
		AspectRatio: "16:9",
		Duration:    &duration,
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  referenceImageURL,
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
		fmt.Println()

		if result.ProviderMetadata != nil {
			if taskID, ok := result.ProviderMetadata["taskId"].(string); ok {
				fmt.Printf("  Task ID: %s\n", taskID)
			}
		}

		fmt.Println()
		fmt.Println("The generated video will have the visual style from the reference image")
		fmt.Println("applied to the content described in the prompt.")
	}

	// Example 2: Using the flash variant for faster generation
	fmt.Println("\n--- Faster Generation with Flash Variant ---\n")

	flashModel, err := prov.VideoModel("wan2.6-r2v-flash")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Generating with wan2.6-r2v-flash (faster, slightly lower quality)...")

	result2, err := flashModel.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A person dancing in the rain",
		AspectRatio: "9:16", // Vertical video for mobile
		Duration:    &duration,
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  referenceImageURL,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	if len(result2.Videos) > 0 {
		video := result2.Videos[0]
		fmt.Println("✓ Flash video generated successfully!")
		fmt.Printf("  URL: %s\n", video.URL)
		fmt.Printf("  Aspect Ratio: 9:16 (vertical)\n")
	}
}
