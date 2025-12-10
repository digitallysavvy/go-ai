package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("claude-sonnet-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Example 1: Analyze PDF Document ===")
	analyzePDF(ctx, model)

	fmt.Println("\n=== Example 2: Extract Information from PDF ===")
	extractPDFInfo(ctx, model)

	fmt.Println("\n=== Example 3: Summarize PDF ===")
	summarizePDF(ctx, model)
}

func analyzePDF(ctx context.Context, model provider.LanguageModel) {
	// Read PDF file
	pdfData, err := os.ReadFile("example.pdf")
	if err != nil {
		log.Printf("Note: example.pdf not found. Place a PDF file to test this example.")
		log.Printf("Creating a sample message without PDF...")
		demonstrateWithoutPDF(ctx, model)
		return
	}

	// Encode to base64
	base64PDF := base64.StdEncoding.EncodeToString(pdfData)

	// Create message with PDF using FileContent
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					Data:     []byte(base64PDF),
					MimeType: "application/pdf",
				},
				types.TextContent{
					Text: "Please analyze this PDF document and provide a summary of its contents.",
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

	fmt.Println("PDF Analysis:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage: %d\n", result.Usage.TotalTokens)
}

func extractPDFInfo(ctx context.Context, model provider.LanguageModel) {
	pdfData, err := os.ReadFile("invoice.pdf")
	if err != nil {
		log.Printf("Note: invoice.pdf not found, skipping extraction example.")
		return
	}

	base64PDF := base64.StdEncoding.EncodeToString(pdfData)

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					Data:     []byte(base64PDF),
					MimeType: "application/pdf",
				},
				types.TextContent{
					Text: `Extract the following information from this invoice:
- Invoice number
- Date
- Vendor name
- Total amount
- Line items (description and cost)

Format as JSON.`,
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

	fmt.Println("Extracted Information:")
	fmt.Println(result.Text)
}

func summarizePDF(ctx context.Context, model provider.LanguageModel) {
	pdfData, err := os.ReadFile("report.pdf")
	if err != nil {
		log.Printf("Note: report.pdf not found, skipping summary example.")
		return
	}

	base64PDF := base64.StdEncoding.EncodeToString(pdfData)

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					Data:     []byte(base64PDF),
					MimeType: "application/pdf",
				},
				types.TextContent{
					Text: "Provide a concise 3-paragraph summary of this report, highlighting the key findings and recommendations.",
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

	fmt.Println("Summary:")
	fmt.Println(result.Text)
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}

func demonstrateWithoutPDF(ctx context.Context, model provider.LanguageModel) {
	fmt.Println("Demonstrating PDF support API structure...")
	fmt.Println("To use: Place a PDF file named 'example.pdf' in this directory")
	fmt.Println("\nExample message structure:")
	fmt.Print(`messages := []types.Message{
    {
        Role: types.RoleUser,
        Content: []types.ContentPart{
            types.FileContent{
                Data:     pdfBytes,  // or base64 encoded data
                MimeType: "application/pdf",
            },
            types.TextContent{
                Text: "Your question about the PDF",
            },
        },
    },
}
`)
}
