package azure

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR99provbMaiCallerHeaderCaseOverride: TS spreads the call headers over
// config.headers() and the ws handshake sets each key with setHeader (names
// case-insensitive), so a caller "Api-Key" replaces the configured "api-key".
// Go kept both spellings in one map and the dialer's map order picked the
// value sent.
func TestR99provbMaiCallerHeaderCaseOverride(t *testing.T) {
	var mu sync.Mutex
	var got []string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		mu.Lock()
		got = append(got, r.Header.Get("Api-Key"))
		mu.Unlock()
		w.WriteHeader(stdhttp.StatusForbidden)
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "configured", ResourceName: "r", MaiBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.TranscriptionModel("mai-transcribe-2-streaming")
	rate := 16000
	for i := 0; i < 16; i++ {
		result, err := model.(provider.TranscriptionStreamer).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newChanAudioStream([]byte{1}),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
			Headers:          map[string]string{"Api-Key": "caller"},
		})
		if err != nil {
			t.Fatal(err)
		}
		for {
			if _, err := result.Stream.Next(); err != nil {
				break
			}
		}
		_ = result.Stream.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("no handshake reached the server")
	}
	for _, v := range got {
		if v != "caller" {
			t.Fatalf("api-key values = %q, want every handshake to send the caller's", got)
		}
	}
}

// TestR99provbMaiImageErrorInfiniteCode: TS maiErrorSchema's code is
// z.union([z.string(), z.number()]).nullish() and z.number() rejects the
// Infinity JSON.parse reads for 1e400, so the message is the status text.
// Go took the body's message.
func TestR99provbMaiImageErrorInfiniteCode(t *testing.T) {
	err := azureMaiImageAPIErrorText(400, "Bad Request", nil, []byte(`{"error":{"code":1e400,"message":"m"}}`))
	if pe, ok := err.(*providererrors.ProviderError); !ok || pe.Message != "Bad Request" || pe.Data != nil {
		t.Errorf("err = %#v, want the status text and no data", err)
	}
	err = azureMaiImageAPIErrorText(400, "Bad Request", nil, []byte(`{"error":{"code":7,"message":"m"}}`))
	if pe, ok := err.(*providererrors.ProviderError); !ok || pe.Message != "m" {
		t.Errorf("err = %#v, want message m", err)
	}
}
