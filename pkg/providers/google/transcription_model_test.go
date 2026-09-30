package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from ai/packages/google/src/transcription/google-transcription-model.test.ts
// `describe('doGenerate', ...)` (1f7835c: Gemini 3.5 Transcribe, unary half).
// The live/WebSocket streaming half (`doStream`) is not implemented: the Go
// SDK's provider.TranscriptionModel interface has no DoStream method.

func newTranscriptionTestServer(t *testing.T, body map[string]interface{}) (*httptest.Server, *map[string]interface{}, *http.Header) {
	t.Helper()
	var captured map[string]interface{}
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	return server, &captured, &gotHeaders
}

func TestTranscription_SendsTranscriptionConfig(t *testing.T) {
	server, captured, headers := newTranscriptionTestServer(t, map[string]interface{}{
		"id":     "interactions/test",
		"status": "completed",
		"steps": []interface{}{
			map[string]interface{}{
				"type": "model_output",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "Hello world."},
				},
			},
		},
		"usage": map[string]interface{}{
			"total_tokens":        float64(10),
			"total_input_tokens":  float64(10),
			"total_output_tokens": float64(0),
		},
	})
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, err := p.TranscriptionModel(ModelGemini35Transcribe)
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"customVocabulary": []interface{}{"Gemini", "Kubernetes"},
				"languageCodes":    []interface{}{"es-ES"},
				"mode":             "SMART",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Text != "Hello world." {
		t.Fatalf("Text = %q, want %q", result.Text, "Hello world.")
	}

	want := map[string]interface{}{
		"model": "gemini-3.5-transcribe",
		"input": []interface{}{
			map[string]interface{}{
				"type":      "audio",
				"data":      "AQIDBA==",
				"mime_type": "audio/wav",
			},
		},
		"generation_config": map[string]interface{}{
			"transcription_config": map[string]interface{}{
				"language_codes":    []interface{}{"es-ES"},
				"custom_vocabulary": []interface{}{"Gemini", "Kubernetes"},
				"mode":              map[string]interface{}{"type": "smart"},
			},
		},
	}
	gotJSON, _ := json.Marshal(*captured)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("request body = %s, want %s", gotJSON, wantJSON)
	}

	if headers.Get("x-goog-api-key") != "test-api-key" {
		t.Fatalf("x-goog-api-key header = %q", headers.Get("x-goog-api-key"))
	}

	googleMeta, ok := result.ProviderMetadata["google"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata = %#v", result.ProviderMetadata)
	}
	usage, ok := googleMeta["usage"].(map[string]interface{})
	if !ok || usage["total_tokens"] != float64(10) {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestTranscription_MapsDiarizationAndWordTimestampsIntoMode(t *testing.T) {
	server, captured, _ := newTranscriptionTestServer(t, map[string]interface{}{
		"status": "completed",
		"steps":  []interface{}{},
	})
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelGemini35Transcribe)
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"diarization": true, "wordTimestamp": true},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	genConfig := (*captured)["generation_config"].(map[string]interface{})
	transcriptionConfig := genConfig["transcription_config"].(map[string]interface{})
	mode := transcriptionConfig["mode"].(map[string]interface{})
	if mode["type"] != "verbatim" || mode["diarization_mode"] != "speaker" {
		t.Fatalf("mode = %#v", mode)
	}
	granularities, ok := mode["timestamp_granularities"].([]interface{})
	if !ok || len(granularities) != 1 || granularities[0] != "word" {
		t.Fatalf("timestamp_granularities = %#v", mode["timestamp_granularities"])
	}
}

func TestTranscription_OmitsGenerationConfigWhenNoOptionsSet(t *testing.T) {
	server, captured, _ := newTranscriptionTestServer(t, map[string]interface{}{
		"status": "completed",
		"steps":  []interface{}{},
	})
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelGemini35Transcribe)
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if _, ok := (*captured)["generation_config"]; ok {
		t.Fatalf("generation_config should be omitted: %#v", *captured)
	}
}

func TestTranscription_ExtractsWordSegmentsFromWordInfoAnnotations(t *testing.T) {
	server, _, _ := newTranscriptionTestServer(t, map[string]interface{}{
		"id":     "interactions/test",
		"status": "completed",
		"steps": []interface{}{
			map[string]interface{}{
				"type": "model_output",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "The quick brown fox.",
						"annotations": []interface{}{
							map[string]interface{}{"type": "word_info", "text": "The", "speaker": "spk:0", "start_offset": "0.100s", "end_offset": "0.100s"},
							map[string]interface{}{"type": "word_info", "text": "quick", "speaker": "spk:0", "start_offset": "0.100s", "end_offset": "0.400s"},
							map[string]interface{}{"type": "word_info", "text": "brown", "speaker": "spk:0", "start_offset": "0.400s", "end_offset": "0.700s"},
							map[string]interface{}{"type": "word_info", "text": "fox.", "speaker": "spk:0", "start_offset": "0.700s", "end_offset": "1s"},
						},
					},
				},
			},
		},
		"usage": map[string]interface{}{"total_input_tokens": float64(64)},
	})
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelGemini35Transcribe)
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1, 2, 3, 4}, MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Text != "The quick brown fox." {
		t.Fatalf("Text = %q", result.Text)
	}
	wantSegments := []struct {
		text       string
		start, end float64
	}{
		{"The", 0.1, 0.1},
		{"quick", 0.1, 0.4},
		{"brown", 0.4, 0.7},
		{"fox.", 0.7, 1},
	}
	if len(result.Segments) != len(wantSegments) {
		t.Fatalf("Segments = %+v", result.Segments)
	}
	for i, want := range wantSegments {
		got := result.Segments[i]
		if got.Text != want.text || got.Start != want.start || got.End != want.end {
			t.Fatalf("Segments[%d] = %+v, want {%q %v %v}", i, got, want.text, want.start, want.end)
		}
	}
}

func TestTranscription_RejectsLiveModelID(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model, err := p.TranscriptionModel(ModelGemini35TranscribeLive)
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}
	_, err = model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1}, MimeType: "audio/wav",
	})
	if err == nil {
		t.Fatal("expected an error for a live model ID")
	}
	if !strings.Contains(err.Error(), "only supports streaming transcription") {
		t.Fatalf("err = %v, want a message about streaming-only support", err)
	}
}

func TestTranscription_AudioBase64PassthroughAndBaseModelMetadata(t *testing.T) {
	server, captured, _ := newTranscriptionTestServer(t, map[string]interface{}{
		"status": "completed",
		"steps":  []interface{}{},
	})
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, err := p.TranscriptionModel(ModelGemini35Transcribe)
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", model.SpecificationVersion())
	}
	if model.Provider() != "google.generative-ai.transcription" {
		t.Fatalf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != ModelGemini35Transcribe {
		t.Fatalf("ModelID() = %q", model.ModelID())
	}

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		AudioBase64: "AQIDBA==",
		MimeType:    "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	input := (*captured)["input"].([]interface{})[0].(map[string]interface{})
	if input["data"] != "AQIDBA==" {
		t.Fatalf("input.data = %v, want the AudioBase64 value passed through directly", input["data"])
	}
	if result.Response == nil || result.Response.ModelID != ModelGemini35Transcribe {
		t.Fatalf("Response = %+v", result.Response)
	}
}
