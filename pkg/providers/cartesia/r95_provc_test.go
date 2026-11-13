package cartesia

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doCreateClientSecret sends JSON.stringify({grants: {stt: true},
// ...{expires_in}}): grants comes first on the wire.
func TestR95provcRealtimeClientSecretBodyOrder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"token":"access-token"}`))
	}))
	defer srv.Close()
	m := newTestRealtimeModel(srv.URL)
	if _, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: intp(60)}); err != nil {
		t.Fatal(err)
	}
	if want := `{"grants":{"stt":true},"expires_in":60}`; body != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

// r95provcCartesiaRefusingServer mints an access token and answers the STT
// WebSocket handshake with 401.
func r95provcCartesiaRefusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/access-token", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":"t"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	return httptest.NewServer(mux)
}

func r95provcDrainError(t *testing.T, stream provider.TranscriptionStream) error {
	t.Helper()
	for i := 0; i < 10; i++ {
		if _, err := stream.Next(); err != nil {
			return err
		}
	}
	t.Fatal("stream did not fail")
	return nil
}

// TS connectToWebSocket: a socket that fails to open fires onSocketError
// (Error('Cartesia streaming transcription error')); an abort before or
// during the handshake fails with abortSignal.reason.
func TestR95provcStreamingDialFailure(t *testing.T) {
	srv := r95provcCartesiaRefusingServer(t)
	defer srv.Close()
	model := newCartesiaRealtimeTestModel(srv.URL)
	opts := func(abort context.Context) *provider.TranscriptionStreamOptions {
		return &provider.TranscriptionStreamOptions{
			Audio:            newChanAudioStream([]byte{1}),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
			AbortSignal:      abort,
		}
	}

	t.Run("refused handshake", func(t *testing.T) {
		result, err := model.DoStream(context.Background(), opts(nil))
		if err != nil {
			t.Fatal(err)
		}
		defer result.Stream.Close() //nolint:errcheck
		if err := r95provcDrainError(t, result.Stream); err == nil || err.Error() != "Cartesia streaming transcription error" {
			t.Fatalf("err = %v, want the TS socket error text", err)
		}
	})

	t.Run("aborted before open", func(t *testing.T) {
		reason := errors.New("r95provc abort reason")
		abort, cancel := context.WithCancelCause(context.Background())
		cancel(reason)
		result, err := model.DoStream(context.Background(), opts(abort))
		if err != nil {
			t.Fatal(err)
		}
		defer result.Stream.Close() //nolint:errcheck
		if err := r95provcDrainError(t, result.Stream); !errors.Is(err, reason) {
			t.Fatalf("err = %v, want the abort reason", err)
		}
	})
}
