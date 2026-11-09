package mistral

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// provg r90 open item: TS mistralTranscriptionResponseSchema reads usage
// counts with z.number(), so a fractional count is kept; Go failed the
// decode (*int). Whole numbers keep the v0.5.2 int type.
func TestR90integTranscriptionFractionalUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"voxtral-mini-latest","text":"hi","language":"en","segments":[],"usage":{"prompt_tokens":2.5,"completion_tokens":3,"total_tokens":5.5,"prompt_audio_seconds":1.5,"request_count":1}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, err := p.TranscriptionModel("voxtral-mini-latest")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1, 2}, MimeType: "audio/wav"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	want := map[string]interface{}{"prompt_tokens": 2.5, "completion_tokens": 3, "total_tokens": 5.5, "prompt_audio_seconds": 1.5, "request_count": 1}
	if !reflect.DeepEqual(res.ProviderUsage, want) {
		t.Fatalf("ProviderUsage = %#v, want %#v", res.ProviderUsage, want)
	}
}

// provg r90 low class: a fractional chat usage count zeroed the Go field; it
// is truncated into the int64 Usage field now (S1).
func TestR90integChatFractionalUsageNotZeroed(t *testing.T) {
	u := convertMistralUsage([]byte(`{"prompt_tokens":2.5,"completion_tokens":3.5,"total_tokens":6}`))
	if u.InputTokens == nil || *u.InputTokens != 2 || u.OutputTokens == nil || *u.OutputTokens != 3 {
		t.Fatalf("usage = %+v, want input 2, output 3", u)
	}
}
