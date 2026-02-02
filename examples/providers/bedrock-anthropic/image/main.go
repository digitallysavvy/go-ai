package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
)

func main() {
	ctx := context.Background()

	// Create Bedrock Anthropic provider
	provider := bedrockAnthropic.New(bedrockAnthropic.Config{
		Region: "us-east-1",
		Credentials: &bedrockAnthropic.AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
	})

	// Get language model
	model, err := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Read image file
	imageData, err := os.ReadFile("image.jpg")
	if err != nil {
		log.Fatalf("Failed to read image: %v", err)
	}

	// Encode to base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Create prompt with image
	prompt := types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.NewImagePart(imageBase64, "image/jpeg"),
					types.NewTextPart("What's in this image? Describe it in detail."),
				},
			},
		},
	}

	// Generate text with image input
	result, err := ai.GenerateText(ctx, ai.GenerateOptions{
		Model:  model,
		Prompt: prompt,
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Display result
	fmt.Println("Image analysis:")
	fmt.Println(result.Text)
	fmt.Println()
	fmt.Printf("Token usage: %d input, %d output\n",
		result.Usage.GetInputTokens(),
		result.Usage.GetOutputTokens())
}
