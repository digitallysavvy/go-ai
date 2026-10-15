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

func TestTranscriptionOptionsValidatedLikeTS(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "whisper-large-v3")
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1}, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"temperature": 2.0}},
	})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) || ia.Message != "invalid groq provider options" {
		t.Fatalf("err = %v", err)
	}
}

func TestTranscriptionErrorUsesEnvelopeMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad audio","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "whisper-large-v3")
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "bad audio" || pe.StatusCode != 400 {
		t.Fatalf("err = %v", err)
	}
}
