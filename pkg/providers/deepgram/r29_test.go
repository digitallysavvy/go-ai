package deepgram

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS sampleRate is z.number(): a fractional value is valid and must not make
// every other speech option vanish.
func TestR29_FractionalSampleRateKeepsOtherOptions(t *testing.T) {
	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("aura-2")
	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "hello", Voice: "helena",
		ProviderOptions: map[string]interface{}{"deepgram": map[string]interface{}{
			"encoding": "mp3", "sampleRate": 22050.5, "mipOptOut": true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "encoding=mp3") || !strings.Contains(query, "mip_opt_out=true") {
		t.Fatalf("query = %s", query)
	}
}
