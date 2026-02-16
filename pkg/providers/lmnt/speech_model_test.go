package lmnt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestSpeechModel_DoGenerate(t *testing.T) {
	// Mock audio data
	mockAudioData := []byte("fake mp3 audio data")

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "test-api-key" {
			t.Errorf("Expected API key 'test-api-key', got '%s'", apiKey)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
		}

		// Verify request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Failed to read request body: %v", err)
		}

		var reqBody lmntSpeechRequest
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Fatalf("Failed to parse request body: %v", err)
		}

		// Return mock audio response
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(mockAudioData)
	}))
	defer server.Close()

	// Create provider with test server URL
	p := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	model, err := p.SpeechModel("default")
	if err != nil {
		t.Fatalf("Failed to create speech model: %v", err)
	}

	t.Run("basic speech synthesis", func(t *testing.T) {
		result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
			Text:  "Hello, world!",
			Voice: "aurora",
		})

		if err != nil {
			t.Fatalf("DoGenerate failed: %v", err)
		}

		// Verify audio data
		if len(result.Audio) == 0 {
			t.Error("Expected audio data, got empty slice")
		}

		// Verify MIME type
		if result.MimeType != "audio/mpeg" {
			t.Errorf("Expected MIME type 'audio/mpeg', got '%s'", result.MimeType)
		}

		// Verify usage metadata
		expectedCharCount := len("Hello, world!")
		if result.Usage.CharacterCount != expectedCharCount {
			t.Errorf("Expected character count %d, got %d", expectedCharCount, result.Usage.CharacterCount)
		}
	})

	t.Run("speech synthesis with speed control", func(t *testing.T) {
		speed := 1.5

		result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
			Text:  "This is faster speech.",
			Voice: "aurora",
			Speed: &speed,
		})

		if err != nil {
			t.Fatalf("DoGenerate failed: %v", err)
		}

		// Verify audio data
		if len(result.Audio) == 0 {
			t.Error("Expected audio data, got empty slice")
		}
	})

	t.Run("speech synthesis without speed (nil)", func(t *testing.T) {
		result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
			Text:  "Normal speed speech.",
			Voice: "aurora",
			Speed: nil, // Explicitly nil
		})

		if err != nil {
			t.Fatalf("DoGenerate failed: %v", err)
		}

		// Verify audio data
		if len(result.Audio) == 0 {
			t.Error("Expected audio data, got empty slice")
		}
	})
}

func TestSpeechModel_ErrorHandling(t *testing.T) {
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

	model, err := p.SpeechModel("default")
	if err != nil {
		t.Fatalf("Failed to create speech model: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "Test text",
		Voice: "aurora",
	})

	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestSpeechModel_ModelInfo(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})

	model, err := p.SpeechModel("default")
	if err != nil {
		t.Fatalf("Failed to create speech model: %v", err)
	}

	sm := model.(*SpeechModel)

	if sm.Provider() != "lmnt" {
		t.Errorf("Expected provider 'lmnt', got '%s'", sm.Provider())
	}

	if sm.ModelID() != "default" {
		t.Errorf("Expected model ID 'default', got '%s'", sm.ModelID())
	}

	if sm.SpecificationVersion() != "v1" {
		t.Errorf("Expected specification version 'v1', got '%s'", sm.SpecificationVersion())
	}
}
