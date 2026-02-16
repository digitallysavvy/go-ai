package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// STTProvider interface for speech-to-text services
type STTProvider interface {
	Transcribe(ctx context.Context, audioFile string, options STTOptions) (*TranscriptionResult, error)
	Translate(ctx context.Context, audioFile string) (*TranscriptionResult, error)
}

// STTOptions configuration for speech recognition
type STTOptions struct {
	Model                  string   // Model to use (whisper-1)
	Language               string   // Language code (e.g., "en", "es")
	Prompt                 string   // Optional context/prompt
	ResponseFormat         string   // json, text, srt, vtt, verbose_json
	Temperature            float64  // Sampling temperature (0-1)
	TimestampGranularities []string // word or segment level timestamps
}

// TranscriptionResult contains the transcribed text and metadata
type TranscriptionResult struct {
	Text     string             `json:"text"`
	Language string             `json:"language,omitempty"`
	Duration float64            `json:"duration,omitempty"`
	Words    []WordTimestamp    `json:"words,omitempty"`
	Segments []SegmentTimestamp `json:"segments,omitempty"`
}

// WordTimestamp represents word-level timing
type WordTimestamp struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// SegmentTimestamp represents segment-level timing
type SegmentTimestamp struct {
	ID    int     `json:"id"`
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// OpenAISTT implements STTProvider for OpenAI Whisper
type OpenAISTT struct {
	apiKey string
	client *http.Client
}

func NewOpenAISTT(apiKey string) *OpenAISTT {
	return &OpenAISTT{
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (stt *OpenAISTT) Transcribe(ctx context.Context, audioFile string, options STTOptions) (*TranscriptionResult, error) {
	// Set defaults
	if options.Model == "" {
		options.Model = "whisper-1"
	}
	if options.ResponseFormat == "" {
		options.ResponseFormat = "verbose_json"
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file
	file, err := os.Open(audioFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("file", filepath.Base(audioFile))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file: %w", err)
	}

	// Add fields
	writer.WriteField("model", options.Model)
	writer.WriteField("response_format", options.ResponseFormat)

	if options.Language != "" {
		writer.WriteField("language", options.Language)
	}
	if options.Prompt != "" {
		writer.WriteField("prompt", options.Prompt)
	}
	if options.Temperature > 0 {
		writer.WriteField("temperature", fmt.Sprintf("%f", options.Temperature))
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/transcriptions", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+stt.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := stt.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var result TranscriptionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (stt *OpenAISTT) Translate(ctx context.Context, audioFile string) (*TranscriptionResult, error) {
	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file
	file, err := os.Open(audioFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("file", filepath.Base(audioFile))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file: %w", err)
	}

	// Add fields
	writer.WriteField("model", "whisper-1")
	writer.WriteField("response_format", "json")

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/translations", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+stt.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := stt.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var result TranscriptionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	stt := NewOpenAISTT(apiKey)
	ctx := context.Background()

	// Note: This example requires actual audio files to work
	// You can generate sample audio using the text-to-speech example
	// or use your own audio files in supported formats:
	// mp3, mp4, mpeg, mpga, m4a, wav, webm

	fmt.Println("=== Speech-to-Text Examples ===")
	fmt.Println("Note: This example requires audio files to transcribe.")
	fmt.Println("Supported formats: mp3, mp4, mpeg, mpga, m4a, wav, webm")
	fmt.Println("Max file size: 25 MB")
	fmt.Println()

	// Check if sample audio file exists
	sampleFile := "sample.mp3"
	if _, err := os.Stat(sampleFile); os.IsNotExist(err) {
		fmt.Printf("Sample file '%s' not found.\n", sampleFile)
		fmt.Println("\nTo test this example:")
		fmt.Println("1. Run the text-to-speech example to generate audio files")
		fmt.Println("2. Or place your own audio file as 'sample.mp3'")
		fmt.Println("\nExample pattern:")
		demoTranscription()
		return
	}

	// Example 1: Basic transcription
	fmt.Println("=== Example 1: Basic Transcription ===")
	result1, err := stt.Transcribe(ctx, sampleFile, STTOptions{})
	if err != nil {
		log.Fatalf("Transcription failed: %v", err)
	}
	fmt.Printf("Text: %s\n", result1.Text)
	if result1.Language != "" {
		fmt.Printf("Language: %s\n", result1.Language)
	}
	fmt.Println()

	// Example 2: With language hint
	fmt.Println("=== Example 2: With Language Hint ===")
	result2, err := stt.Transcribe(ctx, sampleFile, STTOptions{
		Language: "en", // English
	})
	if err != nil {
		log.Printf("Transcription failed: %v", err)
	} else {
		fmt.Printf("Text: %s\n", result2.Text)
		fmt.Println()
	}

	// Example 3: With context prompt
	fmt.Println("=== Example 3: With Context Prompt ===")
	result3, err := stt.Transcribe(ctx, sampleFile, STTOptions{
		Prompt: "This is about AI and technology.",
	})
	if err != nil {
		log.Printf("Transcription failed: %v", err)
	} else {
		fmt.Printf("Text: %s\n", result3.Text)
		fmt.Println()
	}

	// Example 4: Translation (any language to English)
	fmt.Println("=== Example 4: Translation to English ===")
	result4, err := stt.Translate(ctx, sampleFile)
	if err != nil {
		log.Printf("Translation failed: %v", err)
	} else {
		fmt.Printf("Translated text: %s\n", result4.Text)
		fmt.Println()
	}

	fmt.Println("✨ Transcription complete!")
}

func demoTranscription() {
	fmt.Println("\n=== Demo: How Transcription Works ===")

	example := `
	// Step 1: Initialize the STT provider
	stt := NewOpenAISTT(apiKey)

	// Step 2: Transcribe an audio file
	result, err := stt.Transcribe(ctx, "audio.mp3", STTOptions{
		Language: "en",          // Hint for better accuracy
		ResponseFormat: "verbose_json", // Get detailed results
	})

	// Step 3: Use the transcription
	fmt.Printf("Transcribed: %s\n", result.Text)
	fmt.Printf("Language: %s\n", result.Language)

	// For word-level timestamps:
	for _, word := range result.Words {
		fmt.Printf("%s [%.2fs - %.2fs]\n", word.Word, word.Start, word.End)
	}
	`

	fmt.Println(example)

	fmt.Println("\n=== Supported Features ===")
	features := []string{
		"✓ Multiple languages (98+ languages)",
		"✓ Automatic language detection",
		"✓ Translation to English",
		"✓ Word-level timestamps",
		"✓ Segment-level timestamps",
		"✓ Context prompts for accuracy",
		"✓ Multiple output formats (JSON, text, SRT, VTT)",
	}

	for _, feature := range features {
		fmt.Println("  " + feature)
	}

	fmt.Println("\n=== Use Cases ===")
	useCases := []string{
		"• Meeting transcription",
		"• Podcast transcription",
		"• Video captioning",
		"• Voice note conversion",
		"• Accessibility (audio to text)",
		"• Language learning",
		"• Content indexing",
	}

	for _, useCase := range useCases {
		fmt.Println("  " + useCase)
	}
}
