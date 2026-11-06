package googlevertex

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func wantInvalidOptionsMessage(t *testing.T, err error, want string) {
	t.Helper()
	var iae *providererrors.InvalidArgumentError
	if !errors.As(err, &iae) {
		t.Fatalf("error = %v, want InvalidArgumentError", err)
	}
	if iae.Message != want {
		t.Fatalf("message = %q, want %q", iae.Message, want)
	}
}

// TS parseProviderOptions throws `invalid ${provider} provider options`, naming
// the key that was parsed (googleVertex, vertex or google).
func TestVertexInvalidProviderOptionsMessageNamesParsedKey(t *testing.T) {
	p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"googleVertex", "vertex", "google"} {
		bad := map[string]interface{}{key: map[string]interface{}{"languageCodes": "en"}}
		want := "invalid " + key + " provider options"

		_, err := NewGeminiTranscriptionModel(p, "gemini-3.5-transcribe").DoTranscribe(context.Background(),
			&provider.TranscriptionOptions{ProviderOptions: bad})
		wantInvalidOptionsMessage(t, err, want)

		_, err = NewTranscriptionModel(p, "chirp_2").DoTranscribe(context.Background(),
			&provider.TranscriptionOptions{ProviderOptions: bad})
		wantInvalidOptionsMessage(t, err, want)
	}
	for _, key := range []string{"googleVertex", "vertex"} {
		_, _, err := extractVideoProviderOptions(map[string]interface{}{key: map[string]interface{}{"personGeneration": "nope"}})
		wantInvalidOptionsMessage(t, err, "invalid "+key+" provider options")
	}
}

func TestVertexVideoPollOptionsNullGoogleVertexFallsThroughToVertex(t *testing.T) {
	m := &VideoModel{}
	got := m.getPollOptions(map[string]interface{}{
		"googleVertex": nil,
		"vertex":       map[string]interface{}{"pollIntervalMs": 7},
	})
	if got.PollIntervalMs != 7 {
		t.Fatalf("PollIntervalMs = %d, want 7", got.PollIntervalMs)
	}
}

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
