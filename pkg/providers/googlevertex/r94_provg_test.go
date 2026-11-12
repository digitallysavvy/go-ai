package googlevertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Cloud TTS is TS postJsonToApi (`!response.ok` runs the failed-response
// handler): an unfollowed 3xx is the Vertex API error, not a success.
func TestR94provgCloudTTSNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"error":{"code":300,"message":"moved","status":"X"},"audioContent":"AAAA"}`))
	}))
	defer srv.Close()
	model, err := newChirpProvider(t, srv.URL).SpeechModel("chirp-3-hd")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 300 || pe.Message != "moved" {
		t.Fatalf("err = %T %v, want the 300 API error", err, err)
	}
}

// cr-r94-carry-to-provg class 1 sweep: TS sends the Live setup frame with
// JSON.stringify and the MaaS body transform re-serializes with
// JSON.stringify, so <, > and & stay literal.
func TestR94provgJSONStringifyText(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(websocket.Server{
		Handler: func(conn *websocket.Conn) {
			var raw string
			if websocket.Message.Receive(conn, &raw) == nil {
				got <- raw
			}
			_ = websocket.Message.Receive(conn, &raw)
		},
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
	})
	defer srv.Close()
	m := newLiveTestModel(t, srv.URL)
	res, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            &r94provgAudio{block: make(chan struct{})},
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r94provgIntPtr(16000)},
		ProviderOptions:  map[string]interface{}{"vertex": map[string]interface{}{"customVocabulary": []interface{}{"a<b&c"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	go func() {
		for {
			if _, err := res.Stream.Next(); err != nil {
				return
			}
		}
	}()
	select {
	case f := <-got:
		if !strings.Contains(f, `"a<b&c"`) {
			t.Errorf("setup = %s", f)
		}
	case <-time.After(5 * time.Second):
		t.Error("no setup frame")
	}

	out := applyMaasMaxTokensDefault([]byte(`{"model":"meta/llama-4-scout-17b-16e-instruct-maas","messages":[{"role":"user","content":"a<b&c"}]}`))
	if !strings.Contains(string(out), `"a<b&c"`) || !strings.Contains(string(out), `"max_tokens"`) {
		t.Errorf("maas body = %s", out)
	}
}

type r94provgAudio struct{ block chan struct{} }

func (a *r94provgAudio) Next(ctx context.Context) ([]byte, error) {
	select {
	case <-a.block:
	case <-ctx.Done():
	}
	return nil, io.EOF
}
func (a *r94provgAudio) Cancel(error) {}

func r94provgIntPtr(v int) *int { return &v }

// cr-r94-prova-to-provg 2 sweep: a cancelled call fails the Live stream with
// the cancel cause (TS onAbort(abortSignal.reason)).
func TestR94provgAbortCause(t *testing.T) {
	srv := httptest.NewServer(websocket.Server{
		Handler: func(conn *websocket.Conn) {
			var raw string
			for websocket.Message.Receive(conn, &raw) == nil {
			}
		},
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
	})
	defer srv.Close()
	ctx, cancel := context.WithCancelCause(context.Background())
	res, err := newLiveTestModel(t, srv.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: &r94provgAudio{block: make(chan struct{})}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r94provgIntPtr(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)
	reason := errors.New("r94provg user stop")
	cancel(reason)
	for {
		if _, err := res.Stream.Next(); err != nil {
			if !errors.Is(err, reason) {
				t.Fatalf("err = %v, want the cancel cause", err)
			}
			return
		}
	}
}
