package mistral

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS postToApi / postJsonToApi / fetch with response.ok: an unfollowed 3xx
// runs the failed-response handler, so the call fails with the Mistral API
// error instead of parsing the body as a success.
func TestR94provgNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"object":"error","message":"moved","type":"x","param":null,"code":null}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	check := func(name string, err error) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 300 || pe.Message != "moved" {
			t.Errorf("%s: err = %T %v, want the 300 API error", name, err, err)
		}
	}
	_, err := p.ExperimentalRealtime().CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: "voxtral-mini-realtime"})
	check("create session", err)
	check("close session", p.ExperimentalRealtime().CloseSession(context.Background(), "s1"))
	tm, err := p.TranscriptionModel("voxtral-mini-latest")
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	check("transcription", err)
	sm, err := p.SpeechModel("voxtral-mini-tts-latest")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "v"})
	check("speech", err)
}

// cr-r94-carry-to-provg class 1 sweep: TS sends the session.update frame
// with JSON.stringify, so <, > and & stay literal.
func TestR94provgSessionUpdateIsJSONStringifyText(t *testing.T) {
	got := make(chan string, 4)
	srv := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = websocket.Message.Send(conn, `{"type":"session.created"}`)
		for {
			var text string
			if err := websocket.Message.Receive(conn, &text); err != nil {
				return
			}
			got <- text
		}
	}})
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}), ModelVoxtralMiniTranscribeRealtime3)
	res, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            &r87provaAudio{block: make(chan struct{})},
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r94provgIntPtr(16000)},
		ProviderOptions:  map[string]interface{}{"mistral": map[string]interface{}{"contextBias": []interface{}{"a<b&c"}}},
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
			t.Fatalf("session.update = %s", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no session.update")
	}
}

func r94provgIntPtr(v int) *int { return &v }

// cr-r94-prova-to-provg 2 sweep: a cancelled call fails the realtime stream
// with the cancel cause (TS onAbort(abortSignal.reason)).
func TestR94provgAbortCause(t *testing.T) {
	srv := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = websocket.Message.Send(conn, `{"type":"session.created"}`)
		var text string
		for websocket.Message.Receive(conn, &text) == nil {
		}
	}})
	defer srv.Close()
	ctx, cancel := context.WithCancelCause(context.Background())
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}), ModelVoxtralMiniTranscribeRealtime3)
	res, err := m.DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: &r87provaAudio{block: make(chan struct{})}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r94provgIntPtr(16000)},
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
