package fishaudio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
	m, err := p.SpeechModel("s2.1-pro")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "v"})
	if err == nil {
		t.Fatalf("result = %+v, want the 300 API error", res)
	}
}
