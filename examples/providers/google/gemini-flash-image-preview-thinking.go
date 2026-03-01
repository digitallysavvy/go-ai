//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

// Example: gemini-3.1-flash-image-preview with thinking enabled
// Demonstrates image generation using the Gemini 3.1 Flash Image Preview model
// with and without the thinking/reasoning budget option.
//
// The thinking budget tells the model how many tokens it can spend reasoning
// before generating the image, which can improve image quality for complex prompts.
//
// Run with: GOOGLE_GENERATIVE_AI_API_KEY=... go run gemini-flash-image-preview-thinking.go

func main() {
	apiKey := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if apiKey == "" {
		log.Fatal("GOOGLE_GENERATIVE_AI_API_KEY environment variable is required")
	}

	prov := google.New(google.Config{APIKey: apiKey})

	imgModel, err := prov.ImageModel(google.ModelGemini31FlashImagePreview)
	if err != nil {
		log.Fatalf("Failed to create image model: %v", err)
	}

	ctx := context.Background()

	// --- Standard image generation ---
	fmt.Println("=== gemini-3.1-flash-image-preview: Standard Generation ===")
	fmt.Println()

	standardResult, err := imgModel.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "A serene Japanese garden with cherry blossoms, koi pond, and a traditional wooden bridge at sunset",
		AspectRatio: google.ImageAspectRatio16x9,
	})
	if err != nil {
		log.Printf("Standard generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Generated image: %d bytes, MIME type: %s\n", len(standardResult.Image), standardResult.MimeType)
	}
	fmt.Println()

	// --- Image generation with thinking enabled ---
	// thinkingBudget controls how many tokens the model can use for internal
	// reasoning before generating the image. A higher budget improves quality
	// for complex or detailed prompts at the cost of latency.
	fmt.Println("=== gemini-3.1-flash-image-preview: With Thinking Enabled ===")
	fmt.Println()

	thinkingResult, err := imgModel.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "An intricate steampunk city skyline at dusk, featuring ornate brass clockwork towers, airships with glowing lanterns, cobblestone streets with steam vents, and a dramatic sunset casting long shadows",
		AspectRatio: google.ImageAspectRatio16x9,
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				// thinkingConfig enables the model's internal reasoning pass before
				// image generation. thinkingBudget sets the maximum reasoning tokens.
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget": 1024,
				},
			},
		},
	})
	if err != nil {
		log.Printf("Thinking generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Generated image (with thinking): %d bytes, MIME type: %s\n", len(thinkingResult.Image), thinkingResult.MimeType)
	}
	fmt.Println()

	// --- Image generation with thinking + high resolution ---
	fmt.Println("=== gemini-3.1-flash-image-preview: Thinking + 2K Resolution ===")
	fmt.Println()

	highResResult, err := imgModel.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "A photorealistic portrait of a wise elderly scholar in a candlelit library surrounded by ancient manuscripts",
		AspectRatio: google.ImageAspectRatio3x4,
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"imageSize": google.ImageSize2K,
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget": 2048,
				},
			},
		},
	})
	if err != nil {
		log.Printf("High-res thinking generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Generated image (2K + thinking): %d bytes, MIME type: %s\n", len(highResResult.Image), highResResult.MimeType)
	}
}
