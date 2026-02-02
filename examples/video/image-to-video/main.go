package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

func main() {
	ctx := context.Background()

	// Create Google provider
	prov := google.New(google.Config{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})

	// Get video model
	model, err := prov.VideoModel("gemini-2.0-flash")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Read input image
	imageData, err := os.ReadFile("input.jpg")
	if err != nil {
		log.Fatalf("Failed to read input image: %v", err)
	}

	// Generate video from image + text prompt (image-to-video)
	result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
		Model: model,
		Prompt: ai.VideoPrompt{
			Text: "Animate this image with gentle movement and natural motion",
			Image: &ai.VideoPromptImage{
				Data:      imageData,
				MediaType: "image/jpeg",
			},
		},
		AspectRatio: "16:9",
		Duration:    floatPtr(5.0), // 5 second video
	})

	if err != nil {
		log.Fatalf("Video generation failed: %v", err)
	}

	// Save the generated video
	if result.Video != nil {
		outputPath := "output.mp4"
		if err := os.WriteFile(outputPath, result.Video.Data, 0644); err != nil {
			log.Fatalf("Failed to save video: %v", err)
		}
		fmt.Printf("Video saved to %s\n", outputPath)
		fmt.Printf("Media type: %s\n", result.Video.MediaType)
		fmt.Printf("Size: %d bytes\n", len(result.Video.Data))
	}

	// Print warnings if any
	for _, warning := range result.Warnings {
		fmt.Printf("Warning: %s - %s\n", warning.Type, warning.Message)
	}
}

func floatPtr(f float64) *float64 {
	return &f
}
