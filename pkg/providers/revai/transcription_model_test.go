package revai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

var revaiTestAudio = []byte{0, 1, 2, 3, 4}

// revaiJobSubmitFixture mirrors revai/src/__fixtures__/revai-job-submit.json.
var revaiJobSubmitFixture = map[string]interface{}{
	"id":         "test-id",
	"created_on": "2026-02-12T23:26:22.276Z",
	"name":       "audio.mp3",
	"status":     "in_progress",
	"type":       "async",
	"language":   "en",
}

// revaiJobStatusFixture mirrors revai/src/__fixtures__/revai-job-status.json.
var revaiJobStatusFixture = map[string]interface{}{
	"id":               "test-id",
	"created_on":       "2026-02-12T23:26:22.276Z",
	"completed_on":     "2026-02-12T23:26:26.971Z",
	"name":             "audio.mp3",
	"status":           "transcribed",
	"duration_seconds": 2.51,
	"type":             "async",
	"language":         "en",
}

// revaiTranscriptFixture mirrors revai/src/__fixtures__/revai-transcript.json.
var revaiTranscriptFixture = map[string]interface{}{
	"monologues": []map[string]interface{}{
		{
			"speaker": 0,
			"elements": []map[string]interface{}{
				{"type": "text", "value": "Hello", "ts": 0.075, "end_ts": 0.425, "confidence": 0.96},
				{"type": "punct", "value": " "},
				{"type": "text", "value": "from", "ts": 0.425, "end_ts": 0.665, "confidence": 0.98},
				{"type": "punct", "value": " "},
				{"type": "text", "value": "the", "ts": 0.665, "end_ts": 0.785, "confidence": 0.98},
				{"type": "punct", "value": " "},
				{"type": "text", "value": "Sal", "ts": 0.945, "end_ts": 1.105, "confidence": 0.64},
				{"type": "punct", "value": ","},
				{"type": "punct", "value": " "},
				{"type": "text", "value": "A-I-S-D-K", "ts": 1.185, "end_ts": 2.145, "confidence": 0.96},
				{"type": "punct", "value": "."},
			},
		},
	},
}

// newRevaiTestServer sets up an httptest server serving the submit/status/
// transcript fixtures at the fixed job ID "test-id", matching
// revai-transcription-model.test.ts's server setup.
func newRevaiTestServer(t *testing.T, capture func(path string, r *http.Request)) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture(r.URL.Path, r)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/speechtotext/v1/jobs":
			_ = json.NewEncoder(w).Encode(revaiJobSubmitFixture)
		case "/speechtotext/v1/jobs/test-id":
			_ = json.NewEncoder(w).Encode(revaiJobStatusFixture)
		case "/speechtotext/v1/jobs/test-id/transcript":
			_ = json.NewEncoder(w).Encode(revaiTranscriptFixture)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return New(Config{APIKey: "test-api-key", BaseURL: server.URL})
}

// TestTranscriptionModel_PassesModel mirrors "should pass the model".
func TestTranscriptionModel_PassesModel(t *testing.T) {
	var seenConfig string
	var sawMediaFile bool
	p := newRevaiTestServer(t, func(path string, r *http.Request) {
		if path != "/speechtotext/v1/jobs" {
			return
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		seenConfig = r.FormValue("config")
		_, _, err := r.FormFile("media")
		sawMediaFile = err == nil
	})
	model, _ := p.TranscriptionModel(ModelMachine)

	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav"}); err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if !sawMediaFile {
		t.Fatal("expected a `media` multipart file")
	}
	if seenConfig != `{"transcriber":"machine"}` {
		t.Fatalf("config = %q", seenConfig)
	}
}

// TestTranscriptionModel_Headers mirrors "should pass headers".
func TestTranscriptionModel_Headers(t *testing.T) {
	var seenAuth, seenCustom, seenContentType string
	p := newRevaiTestServer(t, func(path string, r *http.Request) {
		if path != "/speechtotext/v1/jobs" {
			return
		}
		seenAuth = r.Header.Get("Authorization")
		seenCustom = r.Header.Get("Custom-Request-Header")
		seenContentType = r.Header.Get("Content-Type")
	})
	model, _ := p.TranscriptionModel(ModelMachine)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: revaiTestAudio, MimeType: "audio/wav",
		Headers: map[string]string{"Custom-Request-Header": "request-header-value"},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenAuth != "Bearer test-api-key" {
		t.Fatalf("authorization = %q", seenAuth)
	}
	if seenCustom != "request-header-value" {
		t.Fatalf("custom header = %q", seenCustom)
	}
	if seenContentType == "" {
		t.Fatalf("expected multipart content type")
	}
}

// TestTranscriptionModel_ExtractsTranscriptionText mirrors "should extract
// the transcription text".
func TestTranscriptionModel_ExtractsTranscriptionText(t *testing.T) {
	p := newRevaiTestServer(t, nil)
	model, _ := p.TranscriptionModel(ModelMachine)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Text != "Hello from the Sal, A-I-S-D-K." {
		t.Fatalf("text = %q", result.Text)
	}
	if result.Language != "en" {
		t.Fatalf("language = %q", result.Language)
	}
}

// TestTranscriptionModel_ResponseMetadata mirrors "should include response
// data with timestamp, modelId and headers".
func TestTranscriptionModel_ResponseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "test-request-id")
		switch r.URL.Path {
		case "/speechtotext/v1/jobs":
			_ = json.NewEncoder(w).Encode(revaiJobSubmitFixture)
		case "/speechtotext/v1/jobs/test-id":
			_ = json.NewEncoder(w).Encode(revaiJobStatusFixture)
		case "/speechtotext/v1/jobs/test-id/transcript":
			_ = json.NewEncoder(w).Encode(revaiTranscriptFixture)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelMachine)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Response.ModelID != ModelMachine {
		t.Fatalf("modelId = %q", result.Response.ModelID)
	}
	if result.Response.Headers["X-Request-Id"] != "test-request-id" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
}

// TestTranscriptionModel_ProviderOptions mirrors passing revai-specific
// provider options through to the job config.
func TestTranscriptionModel_ProviderOptions(t *testing.T) {
	var seenConfig map[string]interface{}
	p := newRevaiTestServer(t, func(path string, r *http.Request) {
		if path != "/speechtotext/v1/jobs" {
			return
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		_ = json.Unmarshal([]byte(r.FormValue("config")), &seenConfig)
	})
	model, _ := p.TranscriptionModel(ModelFusion)

	rush := true
	skipDiarization := true
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: revaiTestAudio, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"revai": TranscriptionModelOptions{
				Rush:            &rush,
				SkipDiarization: &skipDiarization,
				Language:        "en",
				CustomVocabularies: []map[string]interface{}{
					{"phrases": []string{"Vercel", "AI SDK"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenConfig["transcriber"] != "fusion" || seenConfig["rush"] != true || seenConfig["skip_diarization"] != true || seenConfig["language"] != "en" {
		t.Fatalf("config = %#v", seenConfig)
	}
	if _, ok := seenConfig["custom_vocabularies"]; !ok {
		t.Fatalf("custom_vocabularies missing: %#v", seenConfig)
	}
}

// TestTranscriptionModel_JobSubmissionFailed mirrors surfacing a failed job
// submission.
func TestTranscriptionModel_JobSubmissionFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "job-1", "status": "failed"})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelMachine)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav"})
	if err == nil {
		t.Fatal("expected error for failed job submission")
	}
}

// TestTranscriptionModel_JobFailedDuringPolling mirrors surfacing a job that
// fails while being polled.
func TestTranscriptionModel_JobFailedDuringPolling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/speechtotext/v1/jobs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "job-1", "status": "in_progress"})
		case "/speechtotext/v1/jobs/job-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "job-1", "status": "failed"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelMachine)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav"})
	if err == nil {
		t.Fatal("expected error for a job that fails during polling")
	}
}

// TestTranscriptionModel_APIErrors mirrors surfacing Rev.ai's
// {"error":{"message","code"}} error envelope.
func TestTranscriptionModel_APIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "Resource has been exhausted.", "code": 429},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelMachine)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: revaiTestAudio, MimeType: "audio/wav"})
	if err == nil {
		t.Fatal("expected error")
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want *ProviderError", err)
	}
	if providerErr.Message != "Resource has been exhausted." {
		t.Fatalf("message = %q", providerErr.Message)
	}
	if providerErr.ErrorCode != "429" {
		t.Fatalf("error code = %q", providerErr.ErrorCode)
	}
}
