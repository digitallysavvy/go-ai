package azure

import (
	"context"
	"math"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

// TestR99provbMaiTokenSumPlatformInt: a summed MAI token count beyond the
// platform int is clamped instead of an implementation-defined conversion.
func TestR99provbMaiTokenSumPlatformInt(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{{1e19, math.MaxInt}, {-1e19, math.MinInt}, {7.5, 7}} {
		if got := providerutils.WireInt(tc.in); float64(got) != tc.want {
			t.Errorf("WireInt(%v) = %d, want %v", tc.in, got, tc.want)
		}
	}
}

// TestR99provbSpeechNullBody: TS createBinaryResponseHandler fails a null
// body ("Response body is empty", the 204 / 205 reply fetch gives one) and
// returns an empty 200 as empty audio. Go returned empty audio for both.
func TestR99provbSpeechNullBody(t *testing.T) {
	for _, status := range []int{200, 204} {
		server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.WriteHeader(status)
		}))
		p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		speech, _ := p.SpeechModel("mai-voice-2.1")
		_, err = speech.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
		server.Close()
		if (status == 204) != (err != nil && strings.Contains(err.Error(), "Response body is empty")) {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}
