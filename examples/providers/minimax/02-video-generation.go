//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
)

func main() {
	// Create the MiniMax provider. Uses the MINIMAX_API_KEY environment
	// variable when Config.APIKey is empty. Video generation calls
	// https://api.minimax.io by default (Config.VideoBaseURL).
	prov := minimax.New(minimax.Config{})

	model, err := prov.VideoModel(minimax.ModelH3)
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	ctx := context.Background()
	dur := 6.0

	fmt.Println("Generating video from text prompt...")

	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A white kitten chases a butterfly across a sunlit garden.",
		AspectRatio: "16:9",
		Duration:    &dur,
		ProviderOptions: map[string]interface{}{
			"minimax": map[string]interface{}{
				"resolution": "2K",
			},
		},
	})
	if err != nil {
		log.Fatalf("Video generation failed: %v", err)
	}

	fmt.Println("\nVideo generated successfully!")
	fmt.Printf("Video URL:  %s\n", response.Videos[0].URL)
	fmt.Printf("Media Type: %s\n", response.Videos[0].MediaType)

	if meta, ok := response.ProviderMetadata["minimax"].(map[string]interface{}); ok {
		fmt.Printf("\nMiniMax Metadata:\n")
		fmt.Printf("  Task ID: %v\n", meta["taskId"])
		if usage, ok := meta["usage"].(map[string]interface{}); ok {
			fmt.Printf("  Total seconds: %v\n", usage["totalSeconds"])
		}
	}

	if len(response.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range response.Warnings {
			fmt.Printf("  [%s] %s\n", w.Type, w.Details)
		}
	}
}
