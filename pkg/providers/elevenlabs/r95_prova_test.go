package elevenlabs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r95provaRefusingServer answers every WebSocket handshake with 401, like an
// API that rejects the key (ws emits 'error' for the unexpected response).
func r95provaRefusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TS connectToWebSocket: a socket that fails to open is onSocketError, so
// the stream fails with the provider's socket error text (not the Go dial
// error); the dial error stays reachable with errors.Unwrap.
func TestR95provaSpeechRefusedHandshake(t *testing.T) {
	ts := r95provaRefusingServer(t)
	text := speechTexts("Hi ")
	result, err := newSpeechTestModel(t, ts.URL, "eleven_flash_v2_5").DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	_, err = collectSpeech(t, result.Stream)
	if err == nil || err.Error() != "ElevenLabs speech stream error." {
		t.Fatalf("err = %v, want the TS socket error text", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatal("the dial error is not reachable")
	}
}

func TestR95provaTranscriptionRefusedHandshake(t *testing.T) {
	ts := r95provaRefusingServer(t)
	result, err := newRealtimeTestModel(ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newBlockingAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil || err.Error() != "ElevenLabs realtime transcription error." {
		t.Fatalf("err = %v, want the TS socket error text", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatal("the dial error is not reachable")
	}
}

// TS connectToWebSocket: an abort signal that is already aborted (or aborts
// during the handshake) fails the stream with abortSignal.reason.
func TestR95provaSpeechAbortedBeforeOpen(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()
	abort, cancel := context.WithCancelCause(context.Background())
	reason := errors.New("r95prova user stop")
	cancel(reason)
	result, err := newSpeechTestModel(t, server.ts.URL, "eleven_flash_v2_5").DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{
		Text:        speechTexts("Hi "),
		AbortSignal: abort,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	if _, err := collectSpeech(t, result.Stream); err != reason {
		t.Fatalf("err = %v, want the abort reason", err)
	}
}

func TestR95provaTranscriptionAbortedBeforeOpen(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()
	abort, cancel := context.WithCancelCause(context.Background())
	reason := errors.New("r95prova user stop")
	cancel(reason)
	result, err := newRealtimeTestModel(server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newBlockingAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		AbortSignal:      abort,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	if _, err := drainUntilFinishOrError(t, result.Stream); err != reason {
		t.Fatalf("err = %v, want the abort reason", err)
	}
}
