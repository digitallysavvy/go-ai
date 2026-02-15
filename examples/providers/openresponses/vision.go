package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
)

// This example demonstrates image understanding with a vision-capable local LLM
// Prerequisites:
// 1. Install and start LMStudio (or another service)
// 2. Load a vision model (e.g., LLaVA, BakLLaVA, any multimodal model)
// Note: This requires a model that supports vision. Text-only models will not work.

func main() {
	// Create Open Responses provider
	provider := openresponses.New(openresponses.Config{
		BaseURL: "http://localhost:1234/v1",
	})

	// Get language model
	// For LLaVA in LMStudio, use the model name shown in LMStudio
	model, err := provider.LanguageModel("llava-v1.5-7b")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Image Understanding with Local Vision Model ===\n")

	// Example 1: Analyze image from URL
	fmt.Println("Example 1: Analyze Image from URL")
	analyzeImageFromURL(ctx, model)

	fmt.Println("\n\n" + strings.Repeat("=", 50))

	// Example 2: Analyze local image file
	fmt.Println("\n\nExample 2: Analyze Local Image")
	analyzeLocalImage(ctx, model)

	fmt.Println("\n\n" + strings.Repeat("=", 50))

	// Example 3: Compare multiple images
	fmt.Println("\n\nExample 3: Compare Multiple Images")
	compareImages(ctx, model)

	fmt.Println("\n\n" + strings.Repeat("=", 50))

	// Example 4: OCR - Extract text from image
	fmt.Println("\n\nExample 4: Extract Text (OCR)")
	extractTextFromImage(ctx, model)
}

func analyzeImageFromURL(ctx context.Context, model provider.LanguageModel) {
	// Analyze an image from a URL
	imageURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/d/dd/Gfp-wisconsin-madison-the-nature-boardwalk.jpg/640px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg"

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{
					Text: "Describe this scene in detail. What do you see?",
				},
				types.ImageContent{
					URL: imageURL,
				},
			},
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Image URL:", imageURL)
	fmt.Println("\nDescription:")
	fmt.Println(result.Text)
}

func analyzeLocalImage(ctx context.Context, model provider.LanguageModel) {
	// Read local image file
	imageData, err := os.ReadFile("example.jpg")
	if err != nil {
		log.Printf("Note: example.jpg not found. Create an image file to test this example.")
		log.Printf("Error: %v", err)
		return
	}

	// Encode to base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{
					Text: "What's in this image? Describe it in detail.",
				},
				types.ImageContent{
					Image:    []byte(base64Image),
					MimeType: "image/jpeg",
				},
			},
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Local file: example.jpg")
	fmt.Println("\nAnalysis:")
	fmt.Println(result.Text)

	if result.Usage.TotalTokens != nil {
		fmt.Printf("\nToken usage: %d\n", *result.Usage.TotalTokens)
	}
}

func compareImages(ctx context.Context, model provider.LanguageModel) {
	// Compare two images
	image1URL := "https://upload.wikimedia.org/wikipedia/commons/thumb/d/dd/Gfp-wisconsin-madison-the-nature-boardwalk.jpg/320px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg"
	image2URL := "https://upload.wikimedia.org/wikipedia/commons/thumb/3/3f/Fronalpstock_big.jpg/320px-Fronalpstock_big.jpg"

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{
					Text: "Compare these two landscape images. What are the main differences and similarities?",
				},
				types.ImageContent{
					URL: image1URL,
				},
				types.ImageContent{
					URL: image2URL,
				},
			},
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Comparing two landscape images...")
	fmt.Println("\nComparison:")
	fmt.Println(result.Text)
}

func extractTextFromImage(ctx context.Context, model provider.LanguageModel) {
	// Download a sample image with text
	resp, err := http.Get("https://upload.wikimedia.org/wikipedia/commons/thumb/2/2f/Sample_text_document.png/320px-Sample_text_document.png")
	if err != nil {
		log.Printf("Error downloading image: %v", err)
		return
	}
	defer resp.Body.Close()

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading image: %v", err)
		return
	}

	base64Image := base64.StdEncoding.EncodeToString(imageData)

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{
					Text: "Extract all text from this image. Format it as plain text.",
				},
				types.ImageContent{
					Image:    []byte(base64Image),
					MimeType: "image/png",
				},
			},
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Extracting text from image...")
	fmt.Println("\nExtracted Text:")
	fmt.Println(result.Text)
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}
