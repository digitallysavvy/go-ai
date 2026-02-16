package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
)

// Example 5: Multimodal Input (Image + Text)
// Demonstrates vision capabilities with image inputs

func main() {
	// Create Google Vertex AI provider
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || token == "" {
		log.Fatal("Missing required environment variables")
	}

	prov, err := googlevertex.New(googlevertex.Config{
		Project:     project,
		Location:    location,
		AccessToken: token,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use a vision-capable model
	model, err := prov.LanguageModel(googlevertex.ModelGemini15Pro)
	if err != nil {
		log.Fatal(err)
	}

	// Example 1: Using an image URL
	result1, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.ImageContent{
							URL:      "https://upload.wikimedia.org/wikipedia/commons/thumb/d/dd/Gfp-wisconsin-madison-the-nature-boardwalk.jpg/2560px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg",
							MimeType: "image/jpeg",
						},
						types.TextContent{Text: "Describe this image in detail."},
					},
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Image Description (from URL):")
	fmt.Println(result1.Text)
	fmt.Println()

	// Example 2: Using Google Cloud Storage URL (Vertex AI specific)
	// This demonstrates GCS URL support which is unique to Vertex AI
	gcsURL := os.Getenv("GOOGLE_VERTEX_GCS_IMAGE_URL")
	if gcsURL != "" {
		result2, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt: types.Prompt{
				Messages: []types.Message{
					{
						Role: types.RoleUser,
						Content: []types.ContentPart{
							types.ImageContent{
								URL:      gcsURL, // e.g., "gs://my-bucket/image.jpg"
								MimeType: "image/jpeg",
							},
							types.TextContent{Text: "What's in this image?"},
						},
					},
				},
			},
		})
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("Image Description (from GCS):")
		fmt.Println(result2.Text)
		fmt.Println()
	}

	// Example 3: Using base64-encoded image
	// Read an image file and encode it
	imageFile := os.Getenv("IMAGE_PATH")
	if imageFile != "" {
		imageData, err := os.ReadFile(imageFile)
		if err != nil {
			log.Printf("Could not read image file: %v", err)
		} else {
			result3, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{
							Role: types.RoleUser,
							Content: []types.ContentPart{
								types.ImageContent{
									Image:    imageData,
									MimeType: "image/jpeg",
								},
								types.TextContent{Text: "Analyze this image."},
							},
						},
					},
				},
			})
			if err != nil {
				log.Fatal(err)
			}

			fmt.Println("Image Analysis (from base64):")
			fmt.Println(result3.Text)
		}
	}
}
