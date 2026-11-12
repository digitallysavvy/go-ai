package deepgram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func r94provcServer300(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TS postJsonToApi / postToApi check response.ok: an unfollowed 3xx runs the
// failed-response handler, so speech fails instead of returning the body as
// audio.
func TestR94provcSpeechNon2xxFails(t *testing.T) {
	srv := r94provcServer300(t, "audio-bytes")
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, err := p.SpeechModel("aura-2-thalia-en")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "v"})
	if err == nil {
		t.Fatalf("result = %+v, want the 300 API error", res)
	}
}

// TS failed-response handlers read the body as text (TextDecoder drops a
// UTF-8 BOM), so a BOM-prefixed error envelope still gives its message.
func TestR94provcSpeechErrorBodyIsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"err_code":"E1","err_msg":"bad thing"}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("aura-2-thalia-en")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "v"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "bad thing" {
		t.Fatalf("err = %T %v, want message %q", err, err, "bad thing")
	}
}
