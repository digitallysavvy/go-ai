package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/gladia"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("GLADIA_API_KEY")
	if apiKey == "" {
		log.Fatal("GLADIA_API_KEY environment variable is required")
	}

	// Create Gladia provider
	p := gladia.New(gladia.Config{
		APIKey: apiKey,
	})

	// Get transcription model
	model, err := p.TranscriptionModel("whisper-v3")
	if err != nil {
		log.Fatalf("Failed to create transcription model: %v", err)
	}

	// Read audio file
	audioData, err := os.ReadFile("audio.mp3")
	if err != nil {
		log.Fatalf("Failed to read audio file: %v", err)
	}

	fmt.Println("Transcribing audio with timestamps...")

	// Transcribe audio with timestamps enabled
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:      audioData,
		MimeType:   "audio/mpeg",
		Language:   "en",
		Timestamps: true, // Enable timestamps
	})
	if err != nil {
		log.Fatalf("Transcription failed: %v", err)
	}

	// Print results
	fmt.Println("\n=== Transcription Result ===")
	fmt.Println("Full Text:", result.Text)
	fmt.Printf("Duration: %.2f seconds\n", result.Usage.DurationSeconds)

	// Print timestamps
	if len(result.Timestamps) > 0 {
		fmt.Printf("\n=== Timestamps (%d segments) ===\n", len(result.Timestamps))
		for i, ts := range result.Timestamps {
			fmt.Printf("[%3d] [%6.2fs - %6.2fs] %s\n", i+1, ts.Start, ts.End, ts.Text)
		}
	} else {
		fmt.Println("\nNo timestamps returned")
	}
}
