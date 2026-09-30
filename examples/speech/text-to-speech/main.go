package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// TTSProvider interface for text-to-speech services
type TTSProvider interface {
	Synthesize(ctx context.Context, text string, options TTSOptions) ([]byte, error)
}

// TTSOptions configuration for speech synthesis
type TTSOptions struct {
	Voice  string  // Voice ID
	Model  string  // Model to use
	Speed  float64 // Speaking speed (0.25 to 4.0)
	Format string  // Audio format (mp3, opus, aac, flac)
}

// OpenAITTS implements TTSProvider for OpenAI
type OpenAITTS struct {
	apiKey string
	client *http.Client
}

func NewOpenAITTS(apiKey string) *OpenAITTS {
	return &OpenAITTS{
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (tts *OpenAITTS) Synthesize(ctx context.Context, text string, options TTSOptions) ([]byte, error) {
	// Set defaults
	if options.Voice == "" {
		options.Voice = "alloy"
	}
	if options.Model == "" {
		options.Model = "tts-1"
	}
	if options.Speed == 0 {
		options.Speed = 1.0
	}
	if options.Format == "" {
		options.Format = "mp3"
	}

	// Create request
	reqBody := map[string]interface{}{
		"model":           options.Model,
		"input":           text,
		"voice":           options.Voice,
		"speed":           options.Speed,
		"response_format": options.Format,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/speech", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tts.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := tts.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	// Read audio data
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio data: %w", err)
	}

	return audioData, nil
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	tts := NewOpenAITTS(apiKey)
	ctx := context.Background()

	// Example 1: Basic text-to-speech
	fmt.Println("=== Example 1: Basic TTS ===")
	text1 := "Hello! This is a test of the text-to-speech system."
	audio1, err := tts.Synthesize(ctx, text1, TTSOptions{
		Voice: "alloy",
		Model: "tts-1",
	})
	if err != nil {
		log.Fatalf("TTS failed: %v", err)
	}
	saveAudio("output1.mp3", audio1)
	fmt.Printf("✅ Generated speech: %d bytes → output1.mp3\n\n", len(audio1))

	// Example 2: Different voices
	voices := []string{"alloy", "echo", "fable", "onyx", "nova", "shimmer"}
	fmt.Println("=== Example 2: Different Voices ===")
	text2 := "The quick brown fox jumps over the lazy dog."

	for _, voice := range voices {
		audio, err := tts.Synthesize(ctx, text2, TTSOptions{
			Voice: voice,
			Model: "tts-1",
		})
		if err != nil {
			log.Printf("Failed for voice %s: %v", voice, err)
			continue
		}
		filename := fmt.Sprintf("voice_%s.mp3", voice)
		saveAudio(filename, audio)
		fmt.Printf("✅ Voice '%s': %d bytes → %s\n", voice, len(audio), filename)
	}
	fmt.Println()

	// Example 3: Speed variations
	fmt.Println("=== Example 3: Speed Variations ===")
	text3 := "This sentence is spoken at different speeds to demonstrate the speed control feature."
	speeds := []float64{0.5, 1.0, 1.5, 2.0}

	for _, speed := range speeds {
		audio, err := tts.Synthesize(ctx, text3, TTSOptions{
			Voice: "nova",
			Model: "tts-1",
			Speed: speed,
		})
		if err != nil {
			log.Printf("Failed for speed %.1f: %v", speed, err)
			continue
		}
		filename := fmt.Sprintf("speed_%.1fx.mp3", speed)
		saveAudio(filename, audio)
		fmt.Printf("✅ Speed %.1fx: %d bytes → %s\n", speed, len(audio), filename)
	}
	fmt.Println()

	// Example 4: HD quality model
	fmt.Println("=== Example 4: HD Quality (tts-1-hd) ===")
	text4 := "This is generated using the high-definition TTS model for improved audio quality."
	audioHD, err := tts.Synthesize(ctx, text4, TTSOptions{
		Voice: "shimmer",
		Model: "tts-1-hd",
	})
	if err != nil {
		log.Fatalf("HD TTS failed: %v", err)
	}
	saveAudio("output_hd.mp3", audioHD)
	fmt.Printf("✅ HD quality: %d bytes → output_hd.mp3\n\n", len(audioHD))

	// Example 5: Long text
	fmt.Println("=== Example 5: Long Text ===")
	longText := `
	Artificial intelligence is transforming how we interact with technology.
	From voice assistants to autonomous vehicles, AI is becoming increasingly
	integrated into our daily lives. Text-to-speech technology is one example
	of how AI makes information more accessible to everyone.
	`
	audioLong, err := tts.Synthesize(ctx, longText, TTSOptions{
		Voice: "onyx",
		Model: "tts-1",
	})
	if err != nil {
		log.Fatalf("Long text TTS failed: %v", err)
	}
	saveAudio("output_long.mp3", audioLong)
	fmt.Printf("✅ Long text: %d bytes → output_long.mp3\n\n", len(audioLong))

	// Example 6: Different formats
	fmt.Println("=== Example 6: Audio Formats ===")
	text6 := "This audio is saved in different formats."
	formats := []string{"mp3", "opus", "aac", "flac"}

	for _, format := range formats {
		audio, err := tts.Synthesize(ctx, text6, TTSOptions{
			Voice:  "echo",
			Model:  "tts-1",
			Format: format,
		})
		if err != nil {
			log.Printf("Failed for format %s: %v", format, err)
			continue
		}
		filename := fmt.Sprintf("output.%s", format)
		saveAudio(filename, audio)
		fmt.Printf("✅ Format %s: %d bytes → %s\n", format, len(audio), filename)
	}

	fmt.Println("\n✨ All audio files generated successfully!")
	fmt.Println("   Play them with: mpg123 output1.mp3")
}

func saveAudio(filename string, data []byte) error {
	err := os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to save %s: %w", filename, err)
	}
	return nil
}
