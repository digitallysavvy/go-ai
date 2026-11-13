package azure

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR95provbMaiWebSocketOpenFailure (cr-r95-prova-to-provb): TS
// connectToWebSocket fails a socket that does not open (a 401 handshake)
// with the onSocketError text, and an aborted dial with abortSignal.reason.
func TestR95provbMaiWebSocketOpenFailure(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refused.Close()
	m := newAzureMaiTranscriptionModel("mai-transcribe", func() (string, error) { return refused.URL, nil },
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil })
	run := func(ctx context.Context) error {
		res, err := m.DoStream(ctx, &provider.TranscriptionStreamOptions{Audio: &r94provbBlockingAudio{}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}}, AzureTranscriptionModelOptions{}, nil)
		if err != nil {
			return err
		}
		for {
			if _, err := res.Stream.Next(); err != nil {
				return err
			}
		}
	}
	if err := run(context.Background()); err == nil || err.Error() != "Azure MAI transcription WebSocket error" {
		t.Errorf("refused handshake: err = %v", err)
	}
	cause := errors.New("r95provb aborted")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	if err := run(ctx); !errors.Is(err, cause) {
		t.Errorf("aborted dial: err = %v, want the abort cause", err)
	}
}
