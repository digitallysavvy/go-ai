package mistral

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func mistralErrorResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// Speech and transcription used client.Do without checking the status, so a
// 400 turned into an empty successful result.
func TestMistralSpeechAndTranscriptionReturnStatusErrors(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return mistralErrorResponse(400, `{"message":"The request was rejected."}`), nil
	})
	_, err := NewSpeechModel(p, "voxtral-mini-tts-latest").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("speech error = %v", err)
	}
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 400 {
		t.Fatalf("speech error is not a 400 ProviderError: %#v", err)
	}
	_, err = NewTranscriptionModel(p, "voxtral-mini-latest").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("transcription error = %v", err)
	}
}
