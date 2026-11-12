package groq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Transcription is TS postFormDataToApi (`!response.ok` runs the
// failed-response handler): an unfollowed 3xx is the Groq API error, not a
// transcript.
func TestR94provgTranscriptionNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"error":{"message":"moved","type":"x"},"text":"hi"}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("whisper-large-v3")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 300 || pe.Message != "moved" {
		t.Fatalf("err = %T %v, want the 300 API error", err, err)
	}
}
