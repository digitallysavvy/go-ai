package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
)

func main() {
	ctx := context.Background()

	// Create Bedrock Anthropic provider
	p := bedrockAnthropic.New(bedrockAnthropic.Config{
		Region: "us-east-1",
		Credentials: &bedrockAnthropic.AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
	})

	// Get language model
	model, err := p.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Stream text
	stream, err := ai.StreamText(ctx, ai.StreamOptions{
		Model:  model,
		Prompt: "Invent a new holiday and describe its traditions.",
	})
	if err != nil {
		log.Fatalf("Failed to stream text: %v", err)
	}
	defer stream.Close()

	fmt.Println("Streaming text:")
	fmt.Println()

	// Read stream chunks
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Stream error: %v", err)
		}

		switch chunk.Type {
		case provider.ChunkTypeText:
			fmt.Print(chunk.Text)
		case provider.ChunkTypeUsage:
			// Token usage information
		case provider.ChunkTypeFinish:
			fmt.Println()
			fmt.Println()
			fmt.Printf("Finish reason: %s\n", chunk.FinishReason)
		}
	}
}
