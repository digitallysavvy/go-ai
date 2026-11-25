package deepgram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r92provcRawQuery serves one request and returns its raw query string.
func r92provcRawQuery(t *testing.T, body string, contentType string, call func(baseURL string)) string {
	t.Helper()
	var mu sync.Mutex
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		raw = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	call(srv.URL)
	mu.Lock()
	defer mu.Unlock()
	return raw
}

// TestR92provcQueryKeyOrder (r92 provc-08): TS builds both query strings
// with URLSearchParams over a JS object, so the parameters keep the getArgs
// insertion order (a re-set key keeps its place) instead of sorted order.
func TestR92provcQueryKeyOrder(t *testing.T) {
	speed := 1.2
	speech := r92provcRawQuery(t, "audio", "audio/wav", func(base string) {
		m := NewSpeechModel(New(Config{APIKey: "k", BaseURL: base}), "aura-2-helena-en")
		if _, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
			Text: "hi", OutputFormat: "wav_24000", Speed: &speed,
			ProviderOptions: map[string]interface{}{"deepgram": map[string]interface{}{"mipOptOut": true}},
		}); err != nil {
			t.Fatal(err)
		}
	})
	if want := "model=aura-2-helena-en&container=wav&encoding=linear16&sample_rate=24000&mip_opt_out=true&speed=1.2"; speech != want {
		t.Errorf("speech query = %s\nwant          %s", speech, want)
	}

	transcript := `{"metadata":{"duration":1},"results":{"channels":[{"alternatives":[{"transcript":"hi","words":[]}]}]}}`
	listen := r92provcRawQuery(t, transcript, "application/json", func(base string) {
		m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: base}), "nova-3")
		if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte("a"), MimeType: "audio/wav",
			ProviderOptions: map[string]interface{}{"deepgram": map[string]interface{}{"utterances": true, "uttSplit": 0.8, "smartFormat": true, "diarize": true}},
		}); err != nil {
			t.Fatal(err)
		}
	})
	if want := "model=nova-3&diarize=true&smart_format=true&utterances=true&utt_split=0.8"; listen != want {
		t.Errorf("listen query = %s\nwant           %s", listen, want)
	}
}
