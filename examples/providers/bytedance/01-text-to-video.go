//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
)

func main() {
	// Create the ByteDance provider.
	// API key is read from BYTEDANCE_API_KEY environment variable.
	prov, err := bytedance.New(bytedance.Config{
		APIKey: os.Getenv("BYTEDANCE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Get the Seedance 1.0 Pro text-to-video model
	model, err := prov.VideoModel(string(bytedance.ModelSeedance10Pro))
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	ctx := context.Background()
	dur := 5.0

	fmt.Println("Generating video from text prompt...")

	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A cherry blossom tree sways gently in the spring breeze, petals floating through the air",
		AspectRatio: "16:9",
		Duration:    &dur,
	})
	if err != nil {
		log.Fatalf("Video generation failed: %v", err)
	}

	fmt.Println("\nVideo generated successfully!")
	fmt.Printf("Video URL:  %s\n", response.Videos[0].URL)
	fmt.Printf("Media Type: %s\n", response.Videos[0].MediaType)

	// Display provider metadata
	if meta, ok := response.ProviderMetadata["bytedance"].(map[string]interface{}); ok {
		fmt.Printf("\nByteDance Metadata:\n")
		fmt.Printf("  Task ID: %v\n", meta["taskId"])
		if usage, ok := meta["usage"].(map[string]interface{}); ok {
			fmt.Printf("  Tokens: %v\n", usage["completion_tokens"])
		}
	}

	// Display any warnings
	if len(response.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range response.Warnings {
			fmt.Printf("  [%s] %s\n", w.Type, w.Message)
		}
	}
}
