package xai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// provg r90 low class (HTML-escaped request text): TS posts requestBody and
// reports request.body = JSON.stringify(requestBody), in the body literal's
// key order with `<`, `>` and `&` literal (xai-speech-model.ts 127-138).
func TestR90integSpeechRequestBodyText(t *testing.T) {
	var mu sync.Mutex
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = string(b)
		mu.Unlock()
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("grok-tts")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Tom & Jerry <3", Voice: "eve", Language: "en",
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"sampleRate": 24000, "textNormalization": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	const want = `{"text":"Tom & Jerry <3","voice_id":"eve","language":"en","output_format":{"codec":"mp3","sample_rate":24000},"text_normalization":true}`
	if sent != want || res.Request == nil || res.Request.Body != want {
		t.Fatalf("sent %s\nreported %v\nwant %s", sent, res.Request, want)
	}
}
