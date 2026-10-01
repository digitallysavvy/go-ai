package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestSpeechModel_DoGenerate_UsesVersionRelativePath(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model := NewSpeechModel(p, "tts-1")

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if requestPath != "/v1/audio/speech" {
		t.Fatalf("path = %q, want /v1/audio/speech", requestPath)
	}
}

func TestTranscriptionModel_DoTranscribe_UsesVersionRelativePathAndMultipartBody(t *testing.T) {
	var requestPath string
	var contentType string
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		requestBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello","duration":1.2}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model := NewTranscriptionModel(p, "whisper-1")

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio-bytes"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if requestPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", requestPath)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
		t.Fatalf("content type = %q", contentType)
	}
	if !strings.Contains(requestBody, `name="model"`) || !strings.Contains(requestBody, "whisper-1") {
		t.Fatalf("multipart body missing model field: %q", requestBody)
	}
}
