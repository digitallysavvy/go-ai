package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
)

func main() {
	ctx := context.Background()

	// Create Bedrock Anthropic provider
	// Credentials can be provided explicitly or will be loaded from environment/AWS config
	provider := bedrockAnthropic.New(bedrockAnthropic.Config{
		Region: "us-east-1", // or get from environment
		Credentials: &bedrockAnthropic.AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
		// Alternative: Use bearer token
		// BearerToken: os.Getenv("AWS_BEARER_TOKEN_BEDROCK"),
	})

	// Get language model
	model, err := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Generate text
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is quantum computing? Explain in simple terms.",
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Display result
	fmt.Println("Generated text:")
	fmt.Println(result.Text)
	fmt.Println()
	fmt.Printf("Token usage: %d input, %d output, %d total\n",
		result.Usage.GetInputTokens(),
		result.Usage.GetOutputTokens(),
		result.Usage.GetTotalTokens())
	fmt.Printf("Finish reason: %s\n", result.FinishReason)
}
