package gladia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	// TS parses the result with a zod schema that strips unknown keys, so
	// "channel" is not part of providerMetadata.gladia.
	if _, has := firstUtterance["channel"]; has || firstUtterance["speaker"] != float64(0) {
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
	if err == nil || !strings.Contains(err.Error(), "Transcription job failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTranscriptionModel_PollHeadersSurfaced verifies that the headers from
// the final ("done") poll response reach Response.Headers, matching TS
// gladia-transcription-model.ts (transcriptionResultHeaders is threaded
// through to the returned response.headers).
func TestTranscriptionModel_PollHeadersSurfaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"audio_url": "u"})
		case "/v2/pre-recorded":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result_url": "http://" + r.Host + "/v2/result"})
		case "/v2/result":
			w.Header().Set("X-Gladia-Request-Id", "req-123")
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
	if result.Response == nil || result.Response.Headers["X-Gladia-Request-Id"] != "req-123" {
		t.Fatalf("Response.Headers = %#v, want X-Gladia-Request-Id: req-123", result.Response)
	}
}

// TestTranscriptionModel_PollErrorBodyParsed verifies that a non-2xx response
// from the result_url polling GET has its Gladia error envelope decoded, not
// just a generic "failed to download" message (TS uses the same
// gladiaFailedResponseHandler for the polling GET as for upload/init).
func TestTranscriptionModel_PollErrorBodyParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"audio_url": "u"})
		case "/v2/pre-recorded":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result_url": "http://" + r.Host + "/v2/result"})
		case "/v2/result":
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{"message": "job result unavailable", "code": 500},
			})
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
	if err == nil || !strings.Contains(err.Error(), "job result unavailable") {
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

	if tm.Provider() != "gladia.transcription" {
		t.Errorf("Expected provider 'gladia.transcription', got '%s'", tm.Provider())
	}

	if tm.ModelID() != "whisper-v3" {
		t.Errorf("Expected model ID 'whisper-v3', got '%s'", tm.ModelID())
	}

	if tm.SpecificationVersion() != "v4" {
		t.Errorf("Expected specification version 'v4', got '%s'", tm.SpecificationVersion())
	}
}

// TS createJsonResponseHandler on the upload, init and result bodies: a body
// that does not match is "Invalid JSON response"; a done job without a result
// is the TranscriptionResultEmpty error.
func TestTranscriptionModel_ResponseSchemasAndEmptyResult(t *testing.T) {
	run := func(t *testing.T, handler http.HandlerFunc) error {
		t.Helper()
		server := httptest.NewServer(handler)
		defer server.Close()
		model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/v2"}).TranscriptionModel("default")
		_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
		return err
	}
	handler := func(upload, init, result string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/upload"):
				_, _ = w.Write([]byte(upload))
			case strings.HasSuffix(r.URL.Path, "/pre-recorded"):
				_, _ = w.Write([]byte(strings.ReplaceAll(init, "BASE", "http://"+r.Host)))
			default:
				_, _ = w.Write([]byte(result))
			}
		}
	}
	okUpload, okInit := `{"audio_url":"u"}`, `{"result_url":"BASE/v2/pre-recorded/1"}`

	for name, h := range map[string]http.HandlerFunc{
		"upload": handler(`{}`, okInit, `{}`),
		"init":   handler(okUpload, `{"result_url":1}`, `{}`),
		"result": handler(okUpload, okInit, `{"status":"finished"}`),
	} {
		err := run(t, h)
		if !providerutils.IsInvalidJSONResponse(err) {
			t.Errorf("%s: error = %v, want Invalid JSON response", name, err)
		}
	}
	err := run(t, handler(okUpload, okInit, `{"status":"done","result":null}`))
	if err == nil || !strings.Contains(err.Error(), "Transcription result is empty") {
		t.Errorf("empty result: error = %v", err)
	}
}
