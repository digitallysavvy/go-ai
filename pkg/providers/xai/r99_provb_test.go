package xai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR99provbSpeechNullBody: TS createBinaryResponseHandler fails a null
// body ("Response body is empty", a 204 / 205 reply); an empty 200 is empty
// audio. Go returned empty audio for both.
func TestR99provbSpeechNullBody(t *testing.T) {
	for _, status := range []int{200, 204} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("grok-tts")
		_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
		srv.Close()
		if (status == 204) != (err != nil && strings.Contains(err.Error(), "Response body is empty")) {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}
