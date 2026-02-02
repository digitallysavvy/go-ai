package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
)

func main() {
	ctx := context.Background()

	prompt := ai.VideoPrompt{
		Text: "A beautiful mountain landscape with flowing clouds",
	}

	// Example 1: Google Generative AI
	fmt.Println("=== Google Generative AI ===")
	if googleKey := os.Getenv("GOOGLE_API_KEY"); googleKey != "" {
		generateWithGoogle(ctx, prompt)
	} else {
		fmt.Println("Skipped (GOOGLE_API_KEY not set)")
	}

	// Example 2: FAL
	fmt.Println("\n=== FAL ===")
	if falKey := os.Getenv("FAL_KEY"); falKey != "" {
		generateWithFAL(ctx, prompt)
	} else {
		fmt.Println("Skipped (FAL_KEY not set)")
	}

	// Example 3: Replicate
	fmt.Println("\n=== Replicate ===")
	if replicateToken := os.Getenv("REPLICATE_API_TOKEN"); replicateToken != "" {
		generateWithReplicate(ctx, prompt)
	} else {
		fmt.Println("Skipped (REPLICATE_API_TOKEN not set)")
	}
}

func generateWithGoogle(ctx context.Context, prompt ai.VideoPrompt) {
	prov := google.New(google.Config{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})

	model, err := prov.VideoModel("gemini-2.0-flash")
	if err != nil {
		log.Printf("Failed to create Google model: %v", err)
		return
	}

	result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
		Model:       model,
		Prompt:      prompt,
		AspectRatio: "16:9",
	})

	if err != nil {
		log.Printf("Google generation failed: %v", err)
		return
	}

	saveVideo(result, "google_output.mp4")
}

func generateWithFAL(ctx context.Context, prompt ai.VideoPrompt) {
	prov := fal.New(fal.Config{
		APIKey: os.Getenv("FAL_KEY"),
	})

	model := fal.NewVideoModel(prov, "fal-ai/luma-dream-machine")

	result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
		Model:       model,
		Prompt:      prompt,
		AspectRatio: "16:9",
	})

	if err != nil {
		log.Printf("FAL generation failed: %v", err)
		return
	}

	saveVideo(result, "fal_output.mp4")
}

func generateWithReplicate(ctx context.Context, prompt ai.VideoPrompt) {
	prov := replicate.New(replicate.Config{
		APIKey: os.Getenv("REPLICATE_API_TOKEN"),
	})

	model := replicate.NewVideoModel(prov, "minimax/video-01")

	result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
		Model:       model,
		Prompt:      prompt,
		AspectRatio: "16:9",
	})

	if err != nil {
		log.Printf("Replicate generation failed: %v", err)
		return
	}

	saveVideo(result, "replicate_output.mp4")
}

func saveVideo(result *ai.GenerateVideoResult, filename string) {
	if result.Video == nil {
		log.Printf("No video generated for %s", filename)
		return
	}

	if err := os.WriteFile(filename, result.Video.Data, 0644); err != nil {
		log.Printf("Failed to save %s: %v", filename, err)
		return
	}

	fmt.Printf("✓ Saved to %s (%d bytes, %s)\n",
		filename,
		len(result.Video.Data),
		result.Video.MediaType,
	)

	if len(result.Warnings) > 0 {
		fmt.Println("  Warnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  - %s: %s\n", w.Type, w.Message)
		}
	}
}
