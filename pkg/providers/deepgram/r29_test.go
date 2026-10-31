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

// TS sends a fractional sampleRate as String(value) when the encoding does not
// restrict it, and names it verbatim in the "ignored" warning when it does.
func TestR30_FractionalSampleRateIsSent(t *testing.T) {
	cases := []struct {
		encoding  string
		wantQuery string
		wantWarn  string
	}{
		{"", "sample_rate=22050.5", ""},
		{"linear16", "", "Sample rate 22050.5 was ignored."},
	}
	for _, c := range cases {
		var query string
		srv := speechTestServer(t, nil, &query)
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("aura-2")
		dg := map[string]interface{}{"sampleRate": 22050.5}
		if c.encoding != "" {
			dg["encoding"] = c.encoding
		}
		res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
			Text: "hello", Voice: "helena", OutputFormat: "xyz",
			ProviderOptions: map[string]interface{}{"deepgram": dg},
		})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if c.wantQuery != "" && !strings.Contains(query, c.wantQuery) {
			t.Fatalf("%s: query = %s", c.encoding, query)
		}
		if c.wantWarn != "" {
			found := false
			for _, w := range res.Warnings {
				found = found || strings.Contains(w.Details, c.wantWarn)
			}
			if !found || strings.Contains(query, "sample_rate") {
				t.Fatalf("%s: warnings = %+v query = %s", c.encoding, res.Warnings, query)
			}
		}
	}
}
