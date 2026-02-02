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

	// Read PDF file
	pdfData, err := os.ReadFile("document.pdf")
	if err != nil {
		log.Fatalf("Failed to read PDF: %v", err)
	}

	// Encode to base64
	pdfBase64 := base64.StdEncoding.EncodeToString(pdfData)

	// Create prompt with PDF
	prompt := types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.NewFilePart(pdfBase64, "application/pdf"),
					types.NewTextPart("Summarize this PDF document. What are the main points?"),
				},
			},
		},
	}

	// Generate text with PDF input
	result, err := ai.GenerateText(ctx, ai.GenerateOptions{
		Model:  model,
		Prompt: prompt,
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Display result
	fmt.Println("PDF summary:")
	fmt.Println(result.Text)
	fmt.Println()
	fmt.Printf("Token usage: %d total\n", result.Usage.GetTotalTokens())
}
