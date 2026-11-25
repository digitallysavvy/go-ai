package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
)

// TS names the uploaded audio part with mediaTypeToExtension (subtype, not a
// wav fallback) and sends it with the media type as Content-Type.
func TestTranscriptionUploadPartNameAndType(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	}))
	defer srv.Close()

	g, _ := groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("whisper-large-v3")
	_, _ = g.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/aac"})
	if !strings.Contains(body, `filename="audio.aac"`) || !strings.Contains(body, "Content-Type: audio/aac") {
		t.Errorf("groq upload part = %s", body)
	}
	m, _ := mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("voxtral-mini-latest")
	body = ""
	_, _ = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/opus"})
	if !strings.Contains(body, `filename="audio.ogg"`) || !strings.Contains(body, "Content-Type: audio/opus") {
		t.Errorf("mistral upload part = %s", body)
	}
}
