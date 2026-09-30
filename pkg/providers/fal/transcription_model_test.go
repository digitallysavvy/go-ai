package fal

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// newTestTranscriptionModel builds a Provider whose queueHost points at an
// httptest server, mirroring how fal-transcription-model.test.ts intercepts
// "https://queue.fal.run/fal-ai/{modelId}" and
// ".../fal-ai/{modelId}/requests/{id}" with createTestServer.
func newTestTranscriptionModel(t *testing.T, mux *http.ServeMux) (*TranscriptionModel, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mux)
	p := New(Config{APIKey: "test-api-key"})
	p.queueHost = srv.URL
	return NewTranscriptionModel(p, "wizper"), srv
}

// TestFalTranscriptionModel_PassesTheModel ports "should pass the model"
// from fal-transcription-model.test.ts.
func TestFalTranscriptionModel_PassesTheModel(t *testing.T) {
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"Hello from the Versal AISDK.","chunks":[{"timestamp":[0,2.508],"text":"Hello from the Versal AISDK."}]}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("fake-audio-bytes"),
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	audioURL, _ := gotBody["audio_url"].(string)
	if !strings.HasPrefix(audioURL, "data:audio/") {
		t.Fatalf("body.audio_url = %q, want data:audio/... prefix", audioURL)
	}
	if gotBody["task"] != "transcribe" {
		t.Fatalf("body.task = %v", gotBody["task"])
	}
	if gotBody["diarize"] != true {
		t.Fatalf("body.diarize = %v", gotBody["diarize"])
	}
	if gotBody["chunk_level"] != "word" {
		t.Fatalf("body.chunk_level = %v, want word (no providerOptions.fal supplied)", gotBody["chunk_level"])
	}

	// The base64 payload should decode back to the original audio bytes.
	commaIdx := strings.Index(audioURL, ",")
	if commaIdx < 0 {
		t.Fatalf("malformed data URI: %q", audioURL)
	}
	decoded, err := base64.StdEncoding.DecodeString(audioURL[commaIdx+1:])
	if err != nil {
		t.Fatalf("failed to decode audio_url payload: %v", err)
	}
	if string(decoded) != "fake-audio-bytes" {
		t.Fatalf("decoded audio_url payload = %q", decoded)
	}
}

// TestFalTranscriptionModel_PassesHeaders ports "should pass headers".
func TestFalTranscriptionModel_PassesHeaders(t *testing.T) {
	var gotHeaders http.Header
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
		Headers: map[string]string{
			"Custom-Request-Header": "request-header-value",
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if gotHeaders.Get("Authorization") != "Key test-api-key" {
		t.Fatalf("Authorization header = %q", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("Custom-Request-Header") != "request-header-value" {
		t.Fatalf("Custom-Request-Header = %q", gotHeaders.Get("Custom-Request-Header"))
	}
}

// TestFalTranscriptionModel_ExtractsText ports "should extract the
// transcription text".
func TestFalTranscriptionModel_ExtractsText(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"Hello from the Versal AISDK.","chunks":[{"timestamp":[0,2.508],"text":"Hello from the Versal AISDK."}]}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Text != "Hello from the Versal AISDK." {
		t.Fatalf("result.Text = %q", result.Text)
	}
	if len(result.Segments) != 1 || result.Segments[0].Start != 0 || result.Segments[0].End != 2.508 {
		t.Fatalf("result.Segments = %+v", result.Segments)
	}
	if result.DurationInSeconds == nil || *result.DurationInSeconds != 2.508 {
		t.Fatalf("result.DurationInSeconds = %v", result.DurationInSeconds)
	}
}

// TestFalTranscriptionModel_ResponseMetadata ports "should include response
// data with timestamp, modelId and headers".
func TestFalTranscriptionModel_ResponseMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "test-request-id")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Response == nil {
		t.Fatal("expected response metadata")
	}
	if result.Response.ModelID != "wizper" {
		t.Fatalf("response.modelId = %q", result.Response.ModelID)
	}
	if result.Response.Headers["X-Request-Id"] != "test-request-id" {
		t.Fatalf("response.headers[X-Request-Id] = %q", result.Response.Headers["X-Request-Id"])
	}
}

// TestFalTranscriptionModel_ProviderOptionsDefaults verifies
// providerOptions.fal (even empty) applies the schema defaults from
// fal-transcription-model-options.ts, including overriding chunk_level from
// the base "word" to the schema default "segment".
func TestFalTranscriptionModel_ProviderOptionsDefaults(t *testing.T) {
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:           []byte("a"),
		MimeType:        "audio/wav",
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if gotBody["language"] != "en" {
		t.Fatalf("body.language = %v, want en", gotBody["language"])
	}
	if gotBody["chunk_level"] != "segment" {
		t.Fatalf("body.chunk_level = %v, want segment (schema default)", gotBody["chunk_level"])
	}
	if gotBody["version"] != "3" {
		t.Fatalf("body.version = %v, want 3", gotBody["version"])
	}
	if gotBody["batch_size"] != float64(64) {
		t.Fatalf("body.batch_size = %v, want 64", gotBody["batch_size"])
	}
}

// TestFalTranscriptionModel_ProviderOptionsOverrides verifies explicit
// camelCase provider options map to their snake_case wire fields.
func TestFalTranscriptionModel_ProviderOptionsOverrides(t *testing.T) {
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"fal": map[string]interface{}{
				"language":    "fr",
				"diarize":     false,
				"chunkLevel":  "segment",
				"numSpeakers": 2,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if gotBody["language"] != "fr" {
		t.Fatalf("body.language = %v", gotBody["language"])
	}
	if gotBody["diarize"] != false {
		t.Fatalf("body.diarize = %v", gotBody["diarize"])
	}
	if gotBody["num_speakers"] != float64(2) {
		t.Fatalf("body.num_speakers = %v", gotBody["num_speakers"])
	}
}

// TestFalTranscriptionModel_PollsThroughStillInProgress verifies the queue
// polling loop treats {"detail":"Request is still in progress"} as
// "keep polling" rather than a failure, matching fal-transcription-model.ts.
func TestFalTranscriptionModel_PollsThroughStillInProgress(t *testing.T) {
	attempts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"detail":"Request is still in progress"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"done"}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	origInterval := falTranscriptionPollInterval
	falTranscriptionPollInterval = 5 * time.Millisecond
	defer func() { falTranscriptionPollInterval = origInterval }()

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("result.Text = %q", result.Text)
	}
	if attempts < 3 {
		t.Fatalf("attempts = %d, want >= 3", attempts)
	}
}

// TestFalTranscriptionModel_ErrorResponse verifies a non-progress error
// response is surfaced as a ProviderError.
func TestFalTranscriptionModel_ErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"boom","code":400}}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestFalTranscriptionModel_MissingRequestID verifies a queue submit
// response without a request_id surfaces an error rather than polling a
// malformed URL forever.
func TestFalTranscriptionModel_MissingRequestID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})

	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err == nil {
		t.Fatal("expected error for missing request_id")
	}
}
