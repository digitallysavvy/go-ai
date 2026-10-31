//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/heygen"
)

func main() {
	apiKey := os.Getenv("HEYGEN_API_KEY")
	if apiKey == "" {
		log.Fatal("HEYGEN_API_KEY environment variable is required")
	}

	prov := heygen.New(heygen.Config{APIKey: apiKey})
	model, err := prov.VideoModel(heygen.VideoModelHeyGenVideo1)
	if err != nil {
		log.Fatalf("Failed to get video model: %v", err)
	}

	duration := 5.0
	fmt.Println("Animating the first frame with HeyGen...")

	// The image becomes the first frame; the video follows its aspect ratio.
	result, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model: model,
		Prompt: ai.VideoPrompt{
			Text: "The subject moves slowly. Preserve the composition and natural lighting. Quiet ambient sound, no music.",
			Image: &ai.VideoPromptImage{
				URL:       "https://raw.githubusercontent.com/vercel/ai/refs/heads/main/examples/ai-functions/data/comic-cat.png",
				MediaType: "image/png",
			},
		},
		Duration: &duration,
		ProviderOptions: map[string]interface{}{
			"heygen": map[string]interface{}{
				"resolution":        "768p",
				"promptEnhancement": "disabled",
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate video: %v", err)
	}

	for _, video := range result.Videos {
		fmt.Printf("Video URL: %s (media type: %s)\n", video.URL, video.MediaType)
	}
}
