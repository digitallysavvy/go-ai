package gladia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestTranscriptionModel_DoTranscribe(t *testing.T) {
	var (
		seenInitiateBody map[string]interface{}
		seenPollAuth     string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			if r.Header.Get("x-gladia-key") != "test-api-key" {
				t.Errorf("upload: expected API key header, got %q", r.Header.Get("x-gladia-key"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"audio_url": "https://api.gladia.io/file/audio-1",
			})
		case "/v2/pre-recorded":
			_ = json.NewDecoder(r.Body).Decode(&seenInitiateBody)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":         "job-1",
				"result_url": "http://" + r.Host + "/v2/pre-recorded/job-1",
			})
		case "/v2/pre-recorded/job-1":
			seenPollAuth = r.Header.Get("x-gladia-key")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"result": map[string]interface{}{
					"metadata": map[string]interface{}{"audio_duration": 36.74},
					"transcription": map[string]interface{}{
						"full_transcript": "Galileo was an American robotic space program.",
						"languages":       []string{"en"},
						"utterances": []map[string]interface{}{
							{
								"text":  "Galileo was an American robotic space program.",
								"start": 0.14, "end": 5.341,
								"confidence": 0.98, "channel": 0, "speaker": 0, "language": "en",
								"words": []map[string]interface{}{
									{"word": "Galileo", "start": 0.14, "end": 0.64, "confidence": 0.94},
								},
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL + "/v2"})
	model, err := p.TranscriptionModel("default")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("fake audio data"),
		MimeType: "audio/mpeg",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}

	if seenInitiateBody["audio_url"] != "https://api.gladia.io/file/audio-1" {
		t.Fatalf("audio_url = %#v", seenInitiateBody["audio_url"])
	}
	if seenPollAuth != "test-api-key" {
		t.Fatalf("poll auth header = %q, want test-api-key (same-origin poll should be trusted)", seenPollAuth)
	}

	if result.Text != "Galileo was an American robotic space program." {
		t.Fatalf("Text = %q", result.Text)
	}
	if result.Language != "en" {
		t.Fatalf("Language = %q", result.Language)
	}
	if result.Usage.DurationSeconds != 36.74 {
		t.Fatalf("Usage.DurationSeconds = %v", result.Usage.DurationSeconds)
	}
	if len(result.Segments) != 1 || result.Segments[0].Start != 0.14 || result.Segments[0].End != 5.341 {
		t.Fatalf("Segments = %#v", result.Segments)
	}

	meta, ok := result.ProviderMetadata["gladia"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected gladia providerMetadata, got %#v", result.ProviderMetadata)
	}
	res, ok := meta["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result in metadata, got %#v", meta)
	}
	transcription, ok := res["transcription"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected transcription in metadata, got %#v", res)
	}
	utterances, ok := transcription["utterances"].([]interface{})
	if !ok || len(utterances) != 1 {
		t.Fatalf("expected 1 utterance in metadata, got %#v", transcription["utterances"])
	}
	firstUtterance, ok := utterances[0].(map[string]interface{})
	if !ok {
		t.Fatalf("utterance not a map: %#v", utterances[0])
	}
	if firstUtterance["speaker"] != float64(0) || firstUtterance["channel"] != float64(0) {
		t.Fatalf("utterance speaker/channel = %#v", firstUtterance)
	}
	if firstUtterance["confidence"] != 0.98 || firstUtterance["language"] != "en" {
		t.Fatalf("utterance confidence/language = %#v", firstUtterance)
	}
	if _, ok := firstUtterance["words"].([]interface{}); !ok {
		t.Fatalf("utterance words missing: %#v", firstUtterance)
	}

	if result.Response == nil || result.Response.ModelID != "default" {
		t.Fatalf("Response.ModelID = %#v, want default", result.Response)
	}
}

func TestTranscriptionModel_PollTimesOutIsNotHitByDoneFirstPoll(t *testing.T) {
	// Sanity check: when the first poll returns "done", DoTranscribe does not
	// wait for the full poll interval before returning.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"audio_url": "u"})
		case "/v2/pre-recorded":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result_url": "http://" + r.Host + "/v2/result"})
		case "/v2/result":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"result": map[string]interface{}{
					"metadata":      map[string]interface{}{"audio_duration": 1.0},
					"transcription": map[string]interface{}{"full_transcript": "hi", "languages": []string{"en"}, "utterances": []interface{}{}},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v2"})
	model, _ := p.TranscriptionModel("default")

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Text != "hi" {
		t.Fatalf("Text = %q", result.Text)
	}
}

func TestTranscriptionModel_JobFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"audio_url": "u"})
		case "/v2/pre-recorded":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result_url": "http://" + r.Host + "/v2/result"})
		case "/v2/result":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "error_code": 42})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v2"})
	model, _ := p.TranscriptionModel("default")

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/mpeg",
	})
	if err == nil || !strings.Contains(err.Error(), "transcription job failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTranscriptionModel_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "Invalid API key", "code": 401},
		})
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "invalid-key",
		BaseURL: server.URL + "/v2",
	})

	model, err := p.TranscriptionModel("default")
	if err != nil {
		t.Fatalf("Failed to create transcription model: %v", err)
	}

	_, err = model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("fake audio data"),
		MimeType: "audio/mpeg",
	})
	if err == nil {
		t.Fatal("Expected error for unauthorized request, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("unexpected error: %v", err)
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

	if tm.SpecificationVersion() != "v4" {
		t.Errorf("Expected specification version 'v4', got '%s'", tm.SpecificationVersion())
	}
}
