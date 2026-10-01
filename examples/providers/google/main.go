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

	ctx := context.Background()
	prov := google.New(google.Config{APIKey: apiKey})

	fmt.Println("=== Google AI Provider Examples ===")
	fmt.Println()

	// Example 1: Text generation with gemini-3.1-pro-preview.
	fmt.Println("--- Text Generation: gemini-3.1-pro-preview ---")
	langModel, err := prov.LanguageModel(google.ModelGemini31ProPreview)
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	result, err := langModel.DoGenerate(ctx, &provider.GenerateOptions{
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

	// Example 2: Interactions API generation with Google-specific options.
	fmt.Println("--- Interactions API: gemini-2.5-flash ---")
	interactionsModel, err := prov.Interactions(google.InteractionsModelGemini25Flash)
	if err != nil {
		log.Fatalf("Failed to create interactions model: %v", err)
	}
	store := false
	interactionResult, err := interactionsModel.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Give one practical use for multimodal embeddings."},
		ProviderOptions: map[string]interface{}{
			"google": google.GoogleInteractionsProviderOptions{
				Store:              &store,
				ResponseModalities: []string{"text"},
				ThinkingLevel:      "low",
			},
		},
	})
	if err != nil {
		log.Printf("Interactions generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Interactions response: %s\n", interactionResult.Text)
	}
	fmt.Println()

	// Example 3: Text embedding with Google embedding options.
	fmt.Println("--- Embedding: gemini-embedding-001 ---")
	embeddingModel, err := prov.EmbeddingModel(google.EmbeddingModelGeminiEmbedding001)
	if err != nil {
		log.Fatalf("Failed to create embedding model: %v", err)
	}
	dimensions := 768
	embeddingResult, err := embeddingModel.DoEmbed(ctx, "semantic search document about mountain weather", &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"google": google.GoogleEmbeddingProviderOptions{
				TaskType:             "RETRIEVAL_DOCUMENT",
				OutputDimensionality: &dimensions,
			},
		},
	})
	if err != nil {
		log.Printf("Embedding failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Embedding dimensions: %d\n", len(embeddingResult.Embedding))
	}
	fmt.Println()

	// Example 4: Optional fileData embedding. Set GOOGLE_FILE_URI to a Files API URI
	// such as files/abc123 to include provider-hosted content in the request.
	if fileURI := os.Getenv("GOOGLE_FILE_URI"); fileURI != "" {
		fmt.Println("--- Multimodal Embedding with fileData ---")
		fileEmbedding, err := embeddingModel.DoEmbed(ctx, "Summarize the attached document for retrieval.", &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{
				"google": google.GoogleEmbeddingProviderOptions{
					TaskType: "RETRIEVAL_DOCUMENT",
					Content: [][]google.EmbeddingPart{
						{
							google.FileDataEmbeddingPart{
								MimeType: "application/pdf",
								FileURI:  fileURI,
							},
						},
					},
				},
			},
		})
		if err != nil {
			log.Printf("fileData embedding failed (check GOOGLE_FILE_URI / model availability): %v", err)
		} else {
			fmt.Printf("fileData embedding dimensions: %d\n", len(fileEmbedding.Embedding))
		}
		fmt.Println()
	}

	// Example 5: Image generation with gemini-3.1-flash-image-preview.
	fmt.Println("--- Image Generation: gemini-3.1-flash-image-preview ---")
	imgModel, err := prov.ImageModel(google.ModelGemini31FlashImagePreview)
	if err != nil {
		log.Fatalf("Failed to create image model: %v", err)
	}
	fmt.Printf("Image model: %s (provider: %s)\n", imgModel.ModelID(), imgModel.Provider())

	imgResult, err := imgModel.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "A vibrant sunset over mountains with golden light",
		AspectRatio: google.ImageAspectRatio16x9,
	})
	if err != nil {
		log.Printf("Image generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Generated image: %d bytes, mime type: %s\n", len(imgResult.Image), imgResult.MimeType)
	}
	fmt.Println()

	// Example 6: Extended aspect ratio with imageSize option.
	fmt.Println("--- Image Generation with extended aspect ratio (21:9) and 2K resolution ---")
	imgModel2, err := prov.ImageModel(google.ModelGemini25FlashImage)
	if err != nil {
		log.Fatalf("Failed to create image model: %v", err)
	}

	imgResult2, err := imgModel2.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:      "A cinematic wide-angle shot of a futuristic city skyline",
		AspectRatio: google.ImageAspectRatio21x9,
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"imageSize": google.ImageSize2K,
			},
		},
	})
	if err != nil {
		log.Printf("Image generation failed (check API key / model availability): %v", err)
	} else {
		fmt.Printf("Generated image: %d bytes, mime type: %s\n", len(imgResult2.Image), imgResult2.MimeType)
	}
}
