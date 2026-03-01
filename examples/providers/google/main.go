package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

func main() {
	apiKey := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if apiKey == "" {
		log.Fatal("GOOGLE_GENERATIVE_AI_API_KEY environment variable is required")
	}

	prov := google.New(google.Config{APIKey: apiKey})

	fmt.Println("=== Google AI Provider Examples ===")
	fmt.Println()

	// Example 1: Text generation with gemini-3.1-pro-preview (new in #12695)
	fmt.Println("--- Text Generation: gemini-3.1-pro-preview ---")
	langModel, err := prov.LanguageModel(google.ModelGemini31ProPreview)
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	result, err := langModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "What is 2 + 2? Answer in one sentence."},
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("Text generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Response: %s\n", result.Text)
	}
	fmt.Println()

	// Example 2: Image generation with gemini-3.1-flash-image-preview (new in #12883)
	fmt.Println("--- Image Generation: gemini-3.1-flash-image-preview ---")
	imgModel, err := prov.ImageModel(google.ModelGemini31FlashImagePreview)
	if err != nil {
		log.Fatalf("Failed to create image model: %v", err)
	}
	fmt.Printf("Image model: %s (provider: %s)\n", imgModel.ModelID(), imgModel.Provider())

	imgResult, err := imgModel.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "A vibrant sunset over mountains with golden light",
		AspectRatio: google.ImageAspectRatio16x9,
	})
	if err != nil {
		log.Printf("Image generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Generated image: %d bytes, mime type: %s\n", len(imgResult.Image), imgResult.MimeType)
	}
	fmt.Println()

	// Example 3: Extended aspect ratio with imageSize option (new in #12897)
	fmt.Println("--- Image Generation with extended aspect ratio (21:9) and 2K resolution ---")
	imgModel2, err := prov.ImageModel(google.ModelGemini25FlashImage)
	if err != nil {
		log.Fatalf("Failed to create image model: %v", err)
	}

	imgResult2, err := imgModel2.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "A cinematic wide-angle shot of a futuristic city skyline",
		AspectRatio: google.ImageAspectRatio21x9, // Ultra-wide, added in #12897
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"imageSize": google.ImageSize2K, // 2K resolution, added in #12897
			},
		},
	})
	if err != nil {
		log.Printf("Image generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Generated image: %d bytes, mime type: %s\n", len(imgResult2.Image), imgResult2.MimeType)
	}
}
