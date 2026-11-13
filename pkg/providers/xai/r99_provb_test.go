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

// TestR99provbErrorInfiniteCode: TS apiErrorSchema's code is
// z.union([z.string(), z.number()]).nullish() and z.number() rejects the
// Infinity JSON.parse reads for 1e400; no union member then matches, so
// the message is the status text. Go took the body's message.
func TestR99provbErrorInfiniteCode(t *testing.T) {
	if _, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":1e400}}`)); ok {
		t.Error("an infinite code matched the error schema")
	}
	if msg, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":400}}`)); !ok || msg != "m" {
		t.Errorf("finite code: %q, %v", msg, ok)
	}
}
