package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/stability"
)

// TS provider settings accept `headers`; these providers' Go Config had no
// Headers field, so custom headers could not be set provider-wide.
func TestProviderConfigHeadersAreSentOnRequests(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Custom")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artifacts":[{"base64":"YQ=="}]}`))
	}))
	defer srv.Close()
	h := map[string]string{"X-Custom": "yes"}

	speech := func(m provider.SpeechModel) {
		got = ""
		_, _ = m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x", Voice: "v"})
		if got != "yes" {
			t.Errorf("%T: X-Custom = %q", m, got)
		}
	}
	e, _ := elevenlabs.New(elevenlabs.Config{APIKey: "k", BaseURL: srv.URL, Headers: h}).SpeechModel("m")
	speech(e)
	c, _ := cartesia.New(cartesia.Config{APIKey: "k", BaseURL: srv.URL, Headers: h}).SpeechModel("m")
	speech(c)
	hm, _ := hume.New(hume.Config{APIKey: "k", BaseURL: srv.URL, Headers: h}).SpeechModel("")
	speech(hm)

	got = ""
	im, _ := stability.New(stability.Config{APIKey: "k", BaseURL: srv.URL, Headers: h}).ImageModel("m")
	_, _ = im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if got != "yes" {
		t.Errorf("stability: X-Custom = %q", got)
	}
}
