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
