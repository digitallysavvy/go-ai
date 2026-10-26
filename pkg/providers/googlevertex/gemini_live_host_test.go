package googlevertex

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS always dials wss://${vertexHost(location)}/ws/...; an explicit baseURL
// (a proxy, say) must not receive the Live WebSocket and its bearer token.
func TestGeminiLiveWebSocketURLIgnoresBaseURL(t *testing.T) {
	p, err := New(Config{Project: "p", Location: "europe-west4", AccessToken: "t", BaseURL: "https://proxy.example/v1"})
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://europe-west4-aiplatform.googleapis.com/ws/google.cloud.aiplatform.v1.LlmBidiService/BidiGenerateContent"
	if got := geminiLiveWebSocketURL(p, "europe-west4"); got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

// The location becomes the WebSocket host (and the bearer token is sent
// there), so a value that is not a single DNS label is rejected before any
// dial, even when an explicit baseURL skipped the provider's own check.
func TestGeminiTranscriptionModel_DoStream_RejectsInvalidLocation(t *testing.T) {
	prov, err := New(Config{Project: "p", Location: "evil.example/x#", AccessToken: "tok", BaseURL: "http://example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	model := NewGeminiTranscriptionModel(prov, ModelGemini35TranscribeLive)
	_, err = model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	var invalidArg *providererrors.InvalidArgumentError
	if !errors.As(err, &invalidArg) || invalidArg.Field != "location" {
		t.Fatalf("err = %v, want InvalidArgumentError on location", err)
	}
}
