package fishaudio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS combineHeaders(config, {model}, options.headers): a caller header wins
// over the model header.
func TestR33_CallerHeaderOverridesModelHeader(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("model")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()
	m := mustSpeechModel(t, New(Config{APIKey: "k", BaseURL: server.URL}), "s1")
	if _, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x", Headers: map[string]string{"model": "custom"}}); err != nil {
		t.Fatal(err)
	}
	if got != "custom" {
		t.Fatalf("model header = %q", got)
	}
}
