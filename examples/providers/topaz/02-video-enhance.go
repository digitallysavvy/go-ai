//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/topaz"
)

func main() {
	apiKey := os.Getenv("TOPAZ_API_KEY")
	if apiKey == "" {
		log.Fatal("TOPAZ_API_KEY environment variable is required")
	}

	prov := topaz.New(topaz.Config{APIKey: apiKey})

	model, err := prov.VideoModel(topaz.VideoModelProteus)
	if err != nil {
		log.Fatalf("Failed to get video model: %v", err)
	}

	ctx := context.Background()

	fmt.Println("Enhancing video...")

	// Topaz enhances an existing video, so it does not take a text
	// prompt. ai.GenerateVideo uses DoStart/DoStatus directly (Topaz
	// implements provider.VideoModelStarter / VideoModelStatusChecker),
	// polling the Topaz express request to completion.
	result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
		Model: model,
		InputReferences: []ai.VideoReferenceInput{
			{Data: ai.VideoPromptImage{URL: "https://example.com/input.mp4", MediaType: "video/mp4"}},
		},
		Resolution: "1920x1080",
	})
	if err != nil {
		log.Fatalf("Failed to enhance video: %v", err)
	}

	fmt.Println("\nVideo enhanced successfully!")
	for _, video := range result.Videos {
		fmt.Printf("Video URL: %s (media type: %s)\n", video.URL, video.MediaType)
	}

	if topazMeta, ok := result.ProviderMetadata["topaz"].(map[string]interface{}); ok {
		fmt.Printf("Topaz metadata: %#v\n", topazMeta)
	}

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s: %s\n", warning.Type, warning.Details)
		}
	}
}
