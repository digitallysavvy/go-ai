package lmnt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestSpeechModel_DoGenerate(t *testing.T) {
	// Mock audio data
	mockAudioData := []byte("fake mp3 audio data")
	var requests []map[string]interface{}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/v1/ai/speech/bytes" {
			t.Errorf("Expected /v1/ai/speech/bytes path, got %s", r.URL.Path)
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

		var reqBody map[string]interface{}
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Fatalf("Failed to parse request body: %v", err)
		}
		requests = append(requests, reqBody)

		// Return mock audio response
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mockAudioData)
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
		requests = nil
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
		want := map[string]interface{}{
			"model":           "default",
			"text":            "Hello, world!",
			"voice":           "aurora",
			"response_format": "mp3",
		}
		if !reflect.DeepEqual(requests[0], want) {
			t.Fatalf("request body = %#v, want %#v", requests[0], want)
		}

	})

	t.Run("speech synthesis with speed control", func(t *testing.T) {
		requests = nil
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
		if requests[0]["speed"] != 1.5 {
			t.Fatalf("speed = %#v, body = %#v", requests[0]["speed"], requests[0])
		}
	})

	t.Run("speech synthesis without speed (nil)", func(t *testing.T) {
		requests = nil
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
		if _, ok := requests[0]["speed"]; ok {
			t.Fatalf("speed should be omitted when nil: %#v", requests[0])
		}
	})
}

func TestSpeechModelBuildRequestBodyParity(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model, err := p.SpeechModel("aurora")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	sm := model.(*SpeechModel)

	body, warnings := sm.buildRequestBody(&provider.SpeechGenerateOptions{Text: "hello"})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	want := map[string]interface{}{
		"model":           "aurora",
		"text":            "hello",
		"voice":           "ava",
		"response_format": "mp3",
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v, want %#v", body, want)
	}

	speed := 1.3
	body, warnings = sm.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:         "hello",
		Voice:        "luna",
		OutputFormat: "wav",
		Speed:        &speed,
		Language:     "es",
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if body["voice"] != "luna" || body["response_format"] != "wav" || body["speed"] != 1.3 || body["language"] != "es" {
		t.Fatalf("custom body mismatch: %#v", body)
	}

	body, warnings = sm.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:         "hello",
		OutputFormat: "opus",
	})
	if body["response_format"] != "mp3" {
		t.Fatalf("unsupported format should keep mp3 fallback: %#v", body)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "outputFormat" || warnings[0].Details != "Unsupported output format: opus. Using mp3 instead." {
		t.Fatalf("warning mismatch: %#v", warnings)
	}
}

func TestSpeechModelBuildRequestBodyProviderOptionsParity(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model, err := p.SpeechModel("aurora")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	sm := model.(*SpeechModel)

	body, warnings := sm.buildRequestBody(&provider.SpeechGenerateOptions{
		Text: "hello",
		ProviderOptions: map[string]interface{}{
			"lmnt": map[string]interface{}{},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	for key, want := range map[string]interface{}{
		"conversational": false,
		"speed":          1.0,
		"temperature":    1.0,
		"top_p":          1.0,
		"sample_rate":    24000,
	} {
		if body[key] != want {
			t.Fatalf("%s = %#v, want %#v; body = %#v", key, body[key], want, body)
		}
	}

	length := 12.5
	seed := 7
	sampleRate := 16000
	conversational := true
	topP := 0.7
	temperature := 0.8
	providerSpeed := 1.4
	body, warnings = sm.buildRequestBody(&provider.SpeechGenerateOptions{
		Text: "hello",
		ProviderOptions: map[string]interface{}{
			"lmnt": SpeechModelOptions{
				Conversational: &conversational,
				Length:         &length,
				Seed:           &seed,
				Speed:          &providerSpeed,
				Temperature:    &temperature,
				TopP:           &topP,
				SampleRate:     &sampleRate,
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	for key, want := range map[string]interface{}{
		"conversational": true,
		"length":         12.5,
		"seed":           7,
		"speed":          1.4,
		"temperature":    0.8,
		"top_p":          0.7,
		"sample_rate":    16000,
	} {
		if body[key] != want {
			t.Fatalf("%s = %#v, want %#v; body = %#v", key, body[key], want, body)
		}
	}
}

func TestSpeechModel_ErrorHandling(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "Invalid API key"}`))
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

	if sm.SpecificationVersion() != "v4" {
		t.Errorf("Expected specification version 'v4', got '%s'", sm.SpecificationVersion())
	}
}
