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
	fmt.Println("Generating from a reference with HeyGen...")

	// References are addressed in the prompt as <Picture 1>, <Video 1>, <Audio 1>.
	// Include a MediaType so the provider can select the reference kind.
	result, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model: model,
		Prompt: ai.VideoPrompt{
			Text: "The character in <Picture 1> walks through a quiet garden. Keep its appearance consistent. Birds chirp softly.",
		},
		InputReferences: []ai.VideoReferenceInput{
			{Data: ai.VideoPromptImage{
				URL:       "https://raw.githubusercontent.com/vercel/ai/refs/heads/main/examples/ai-functions/data/comic-cat.png",
				MediaType: "image/png",
			}},
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
}
