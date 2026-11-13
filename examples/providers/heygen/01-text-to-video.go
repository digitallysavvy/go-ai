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
	fmt.Println("Generating video with HeyGen...")

	// ai.GenerateVideo uses DoStart/DoStatus and polls until the video is ready.
	result, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model: model,
		Prompt: ai.VideoPrompt{
			Text: "A paper boat floats down a quiet stream. Static camera, soft morning light, the sound of flowing water. No music.",
		},
		Duration:    &duration,
		AspectRatio: "16:9",
	})
	if err != nil {
		log.Fatalf("Failed to generate video: %v", err)
	}

	for _, video := range result.Videos {
		fmt.Printf("Video URL: %s (media type: %s)\n", video.URL, video.MediaType)
	}
	fmt.Printf("HeyGen metadata: %#v\n", result.ProviderMetadata["heygen"])
}
