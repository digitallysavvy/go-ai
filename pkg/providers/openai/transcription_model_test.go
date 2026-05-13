package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGetExtensionFromMimeType(t *testing.T) {
	tests := map[string]string{
		"audio/mpeg": "mp3",
		"audio/mp3":  "mp3",
		"audio/wav":  "wav",
		"audio/webm": "webm",
		"audio/mp4":  "m4a",
		"audio/m4a":  "m4a",
		"audio/ogg":  "audio",
	}
	for in, want := range tests {
		if got := getExtensionFromMimeType(in); got != want {
			t.Fatalf("mime %q -> %q, want %q", in, got, want)
		}
	}
}

func TestTranscriptionBuildMultipartBody(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "whisper-1")

	body, ct, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:      []byte("audio"),
		MimeType:   "audio/mpeg",
		Language:   "fr",
		Timestamps: true,
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	if !strings.Contains(ct, "multipart/form-data") {
		t.Fatalf("content type = %q", ct)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, `name="model"`) || !strings.Contains(blob, "whisper-1") {
		t.Fatalf("missing model field: %s", blob)
	}
	if !strings.Contains(blob, `name="language"`) || !strings.Contains(blob, "fr") {
		t.Fatalf("missing language field: %s", blob)
	}
	if !strings.Contains(blob, `name="timestamp_granularities[]"`) || !strings.Contains(blob, `name="response_format"`) || !strings.Contains(blob, "verbose_json") {
		t.Fatalf("missing timestamp or response format fields: %s", blob)
	}
}

func TestTranscriptionDoTranscribeSuccessAndErrors(t *testing.T) {
	var seenPath string
	var seenContentType string
	var seenBody string
	serverURL, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello","duration":1.2}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL + "/v1"})
	m := NewTranscriptionModel(p, "whisper-1")

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if seenPath != "/v1/audio/transcriptions" || !strings.Contains(seenContentType, "multipart/form-data") {
		t.Fatalf("request mismatch path=%q contentType=%q", seenPath, seenContentType)
	}
	if !strings.Contains(seenBody, `name="response_format"`) || !strings.Contains(seenBody, "json") {
		t.Fatalf("response format field missing: %s", seenBody)
	}
	if result.Text != "hello" || result.Usage.DurationSeconds != 1.2 {
		t.Fatalf("result mismatch: %#v", result)
	}

	errorServerURL, closeErr := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad"))
	}))
	defer closeErr()
	mErr := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: errorServerURL + "/v1"}), "whisper-1")
	if _, err := mErr.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"}); err == nil || !strings.Contains(err.Error(), "returned status 400") {
		t.Fatalf("expected status error, got %v", err)
	}

	badJSONServerURL, closeBadJSON := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer closeBadJSON()
	mBadJSON := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: badJSONServerURL + "/v1"}), "whisper-1")
	if _, err := mBadJSON.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"}); err == nil || !strings.Contains(err.Error(), "failed to decode transcription response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}
