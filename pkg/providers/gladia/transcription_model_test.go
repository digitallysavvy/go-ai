package gladia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestTranscriptionModel_DoTranscribe(t *testing.T) {
	// Mock API response
	mockResponse := gladiaTranscriptionResponse{
		Result: struct {
			Transcription struct {
				FullTranscript string `json:"full_transcript"`
				Utterances     []struct {
					Text  string  `json:"text"`
					Start float64 `json:"start"`
					End   float64 `json:"end"`
				} `json:"utterances"`
			} `json:"transcription"`
		}{
			Transcription: struct {
				FullTranscript string `json:"full_transcript"`
				Utterances     []struct {
					Text  string  `json:"text"`
					Start float64 `json:"start"`
					End   float64 `json:"end"`
				} `json:"utterances"`
			}{
				FullTranscript: "Galileo was an American robotic space program that studied the planet Jupiter and its moons.",
				Utterances: []struct {
					Text  string  `json:"text"`
					Start float64 `json:"start"`
					End   float64 `json:"end"`
				}{
					{
						Text:  "Galileo was an American robotic space program",
						Start: 0.14,
						End:   5.341,
					},
					{
						Text:  "that studied the planet Jupiter and its moons.",
						Start: 5.662,
						End:   8.099,
					},
				},
			},
		},
		Metadata: struct {
			Duration float64 `json:"duration"`
		}{
			Duration: 36.74,
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		apiKey := r.Header.Get("x-gladia-key")
		if apiKey != "test-api-key" {
			t.Errorf("Expected API key 'test-api-key', got '%s'", apiKey)
		}

		// Return mock response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Create provider with test server URL
	p := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	model, err := p.TranscriptionModel("whisper-v3")
	if err != nil {
		t.Fatalf("Failed to create transcription model: %v", err)
	}

	t.Run("basic transcription", func(t *testing.T) {
		audioData := []byte("fake audio data")

		result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio:    audioData,
			MimeType: "audio/mpeg",
			Language: "en",
		})

		if err != nil {
			t.Fatalf("DoTranscribe failed: %v", err)
		}

		// Verify transcription text
		expectedText := "Galileo was an American robotic space program that studied the planet Jupiter and its moons."
		if result.Text != expectedText {
			t.Errorf("Expected text '%s', got '%s'", expectedText, result.Text)
		}

		// Verify usage metadata
		if result.Usage.DurationSeconds != 36.74 {
			t.Errorf("Expected duration 36.74, got %f", result.Usage.DurationSeconds)
		}

		// Verify no timestamps by default
		if len(result.Timestamps) != 0 {
			t.Errorf("Expected no timestamps, got %d", len(result.Timestamps))
		}
	})

	t.Run("transcription with timestamps", func(t *testing.T) {
		audioData := []byte("fake audio data")

		result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio:      audioData,
			MimeType:   "audio/mpeg",
			Language:   "en",
			Timestamps: true,
		})

		if err != nil {
			t.Fatalf("DoTranscribe failed: %v", err)
		}

		// Verify timestamps are included
		if len(result.Timestamps) != 2 {
			t.Errorf("Expected 2 timestamps, got %d", len(result.Timestamps))
		}

		if len(result.Timestamps) >= 1 {
			firstTimestamp := result.Timestamps[0]
			if firstTimestamp.Text != "Galileo was an American robotic space program" {
				t.Errorf("Unexpected first timestamp text: %s", firstTimestamp.Text)
			}
			if firstTimestamp.Start != 0.14 {
				t.Errorf("Expected start time 0.14, got %f", firstTimestamp.Start)
			}
			if firstTimestamp.End != 5.341 {
				t.Errorf("Expected end time 5.341, got %f", firstTimestamp.End)
			}
		}
	})
}

func TestTranscriptionModel_ErrorHandling(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "invalid-key",
		BaseURL: server.URL,
	})

	model, err := p.TranscriptionModel("whisper-v3")
	if err != nil {
		t.Fatalf("Failed to create transcription model: %v", err)
	}

	audioData := []byte("fake audio data")

	_, err = model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    audioData,
		MimeType: "audio/mpeg",
	})

	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestTranscriptionModel_ModelInfo(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})

	model, err := p.TranscriptionModel("whisper-v3")
	if err != nil {
		t.Fatalf("Failed to create transcription model: %v", err)
	}

	tm := model.(*TranscriptionModel)

	if tm.Provider() != "gladia" {
		t.Errorf("Expected provider 'gladia', got '%s'", tm.Provider())
	}

	if tm.ModelID() != "whisper-v3" {
		t.Errorf("Expected model ID 'whisper-v3', got '%s'", tm.ModelID())
	}

	if tm.SpecificationVersion() != "v1" {
		t.Errorf("Expected specification version 'v1', got '%s'", tm.SpecificationVersion())
	}
}
