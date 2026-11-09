package openai

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
// key order with `<`, `>` and `&` literal (openai-speech-model.ts). Go sent
// and reported json.Marshal of a map: sorted keys, & escapes.
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
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("tts-1")
	if err != nil {
		t.Fatal(err)
	}
	speed := 1.5
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Tom & Jerry <3", Voice: "nova", Speed: &speed, Instructions: "calm"})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"model":"tts-1","input":"Tom & Jerry <3","voice":"nova","response_format":"mp3","speed":1.5,"instructions":"calm"}`
	mu.Lock()
	defer mu.Unlock()
	if sent != want || res.Request == nil || res.Request.Body != want {
		t.Fatalf("sent %s\nreported %v\nwant %s", sent, res.Request, want)
	}
}
