//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/klingai"
)

// This example demonstrates the KlingAI v3.0 model IDs for both text-to-video
// and image-to-video generation. The v3.0 models offer improved visual quality,
// better coherence, and stronger prompt following compared to earlier versions.
//
// Run with:
//
//	go run 06-v3-models.go
//
// Required environment variables:
//
//	KLINGAI_ACCESS_KEY
//	KLINGAI_SECRET_KEY
func main() {
	// Create KlingAI provider (credentials from environment variables)
	prov, err := klingai.New(klingai.Config{})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()

	// --- Text-to-Video with v3.0 ---
	//
	// Use klingai.KlingV3T2V ("kling-v3.0-t2v") for the latest text-to-video model.
	t2vModel, err := prov.VideoModel(klingai.KlingV3T2V)
	if err != nil {
		log.Fatalf("Failed to get v3.0 t2v model: %v", err)
	}

	fmt.Printf("Model: %s (image-to-video: %v)\n", t2vModel.ModelID(), t2vModel.(*klingai.VideoModel).IsImageToVideo())

	duration := 5.0
	fmt.Println("\nGenerating text-to-video with kling-v3.0-t2v...")

	t2vResp, err := t2vModel.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A futuristic city at sunrise with flying vehicles, cinematic quality",
		AspectRatio: "16:9",
		Duration:    &duration,
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				"mode":           "std",
				"negativePrompt": "low quality, blurry, distorted",
			},
		},
	})
	if err != nil {
		log.Fatalf("T2V generation failed: %v", err)
	}

	fmt.Println("Text-to-video generated successfully!")
	fmt.Printf("Video URL: %s\n", t2vResp.Videos[0].URL)
	if metadata, ok := t2vResp.ProviderMetadata["klingai"].(map[string]interface{}); ok {
		fmt.Printf("Task ID: %s\n", metadata["taskId"])
	}

	// --- Image-to-Video with v3.0 ---
	//
	// Use klingai.KlingV3I2V ("kling-v3.0-i2v") for the latest image-to-video model.
	i2vModel, err := prov.VideoModel(klingai.KlingV3I2V)
	if err != nil {
		log.Fatalf("Failed to get v3.0 i2v model: %v", err)
	}

	fmt.Printf("\nModel: %s (image-to-video: %v)\n", i2vModel.ModelID(), i2vModel.(*klingai.VideoModel).IsImageToVideo())

	fmt.Println("\nGenerating image-to-video with kling-v3.0-i2v...")

	i2vResp, err := i2vModel.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "The camera slowly pans across the scene revealing depth",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/landscape.png", // Replace with a real image URL
		},
		Duration: &duration,
		ProviderOptions: map[string]interface{}{
			"klingai": map[string]interface{}{
				"mode": "std",
			},
		},
	})
	if err != nil {
		log.Fatalf("I2V generation failed: %v", err)
	}

	fmt.Println("Image-to-video generated successfully!")
	fmt.Printf("Video URL: %s\n", i2vResp.Videos[0].URL)
	if metadata, ok := i2vResp.ProviderMetadata["klingai"].(map[string]interface{}); ok {
		fmt.Printf("Task ID: %s\n", metadata["taskId"])
	}
}
