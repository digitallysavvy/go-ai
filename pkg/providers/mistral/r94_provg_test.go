package mistral

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
