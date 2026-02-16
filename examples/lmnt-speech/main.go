package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/lmnt"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("LMNT_API_KEY")
	if apiKey == "" {
		log.Fatal("LMNT_API_KEY environment variable is required")
	}

	// Create LMNT provider
	p := lmnt.New(lmnt.Config{
		APIKey: apiKey,
	})

	// Get speech model
	model, err := p.SpeechModel("default")
	if err != nil {
		log.Fatalf("Failed to create speech model: %v", err)
	}

	// Text to synthesize
	text := "Hello from the Go AI SDK!"

	fmt.Println("Generating speech...")

	// Generate speech
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  text,
		Voice: "aurora",
	})
	if err != nil {
		log.Fatalf("Speech generation failed: %v", err)
	}

	// Save audio to file
	outputFile := "output.mp3"
	if err := os.WriteFile(outputFile, result.Audio, 0644); err != nil {
		log.Fatalf("Failed to save audio file: %v", err)
	}

	// Print results
	fmt.Println("\n=== Speech Generation Result ===")
	fmt.Printf("Audio saved to: %s\n", outputFile)
	fmt.Printf("Audio size: %d bytes\n", len(result.Audio))
	fmt.Printf("MIME type: %s\n", result.MimeType)
	fmt.Printf("Character count: %d\n", result.Usage.CharacterCount)
}
