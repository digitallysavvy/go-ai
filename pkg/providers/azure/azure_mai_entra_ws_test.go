package azure

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// Port of TS "MAI connection settings > sends an Entra bearer token" (the
// recorder cannot capture it: the TS test drives a mock WebSocket, no HTTP).
// The WebSocket handshake carries `Authorization: Bearer <token>` and no
// api-key header. The sibling TS case "rejects Entra bearer auth without a
// custom WebSocket" has no Go counterpart: Go always dials a WebSocket that
// can send headers, so there is no native-WebSocket path to reject.
func TestAzureMaiStreamingSendsEntraBearerToken(t *testing.T) {
	headers := make(chan stdhttp.Header, 1)
	server := httptest.NewServer(websocket.Server{
		Handshake: func(_ *websocket.Config, r *stdhttp.Request) error {
			headers <- r.Header.Clone()
			return nil
		},
		Handler: func(conn *websocket.Conn) { _ = conn.Close() },
	})
	defer server.Close()

	p, err := New(Config{
		ResourceName:    "r",
		MaiBaseURL:      server.URL,
		ADTokenProvider: func(context.Context) (string, error) { return "entra-token", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.TranscriptionModel("MAI-Transcribe-2-Streaming")
	if err != nil {
		t.Fatal(err)
	}
	streamer := model.(provider.TranscriptionStreamer)
	rate := 24000
	result, err := streamer.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck

	select {
	case h := <-headers:
		if got := h.Get("Authorization"); got != "Bearer entra-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := h.Get("Api-Key"); got != "" {
			t.Fatalf("api-key header must not be sent with a token provider, got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no WebSocket handshake")
	}
}
