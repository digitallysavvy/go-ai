package deepgram

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// JSON.stringify leaves HTML characters literal in request.body.
func TestSpeechRequestBodyKeepsHTMLCharacters(t *testing.T) {
	srv := speechTestServer(t, nil, nil)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("aura-2-thalia-en")
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "a<b & c>"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"text":"a<b & c>"}`; res.Request.Body != want {
		t.Fatalf("request.body = %s, want %s", res.Request.Body, want)
	}
}
