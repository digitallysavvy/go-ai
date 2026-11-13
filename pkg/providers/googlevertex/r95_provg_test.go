package googlevertex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r95provgFirstErr(t *testing.T, next func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for {
			if err := next(); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not end")
		return nil
	}
}

// TS onSocketError: a Live socket that fails to open (a 401 handshake) is
// "Vertex Live transcription error." (the native-WebSocket hint is left out,
// S3); a signal aborted before the open is the reason itself.
func TestR95provgLiveRefusedHandshakeAndPreAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	res, err := newLiveTestModel(t, srv.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r95provgFirstErr(t, func() error { _, err := res.Stream.Next(); return err }); got == nil || got.Error() != "Vertex Live transcription error." {
		t.Errorf("err = %v", got)
	}

	reason := errors.New("r95provg aborted before open")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	res, err = newLiveTestModel(t, srv.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err == nil {
		if got := r95provgFirstErr(t, func() error { _, err := res.Stream.Next(); return err }); got != reason {
			t.Errorf("pre-aborted: err = %v, want the reason", got)
		}
	} else if err != reason {
		t.Errorf("pre-aborted: DoStream err = %v", err)
	}
}

// TS cancel() runs cleanup(), which cancels the caller's audio: also when
// the consumer closes the stream while stream-start is still unread.
func TestR95provgLiveCloseWhileEmitPendingCancelsAudio(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	audio := &chanAudioStream{chunks: make(chan []byte)}
	res, err := newLiveTestModel(t, server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, 2*time.Second)
	time.Sleep(20 * time.Millisecond)
	_ = res.Stream.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		audio.mu.Lock()
		c := audio.cancelled
		audio.mu.Unlock()
		if c {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("audio not cancelled after Close with a pending part")
}

// The Vertex Gemini transcription option fields are .optional(): an
// explicit null is invalid.
func TestR95provgTranscriptionOptionNullRejected(t *testing.T) {
	for _, key := range []string{"languageCodes", "customVocabulary", "wordTimestamp", "diarization", "mode"} {
		if _, err := parseVertexGeminiTranscriptionOptionsChecked(map[string]interface{}{"vertex": map[string]interface{}{key: nil}}); err == nil {
			t.Errorf("%s: null accepted", key)
		}
	}
}
