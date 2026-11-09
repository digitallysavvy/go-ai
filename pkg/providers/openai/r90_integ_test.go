package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// provb r90 open item: TS openai-compatible image edits post
// convertToFormData({model, prompt, image, mask, n, size, ...args}): a
// provider option named like one of these keys replaces the value in its
// place instead of being appended a second time.
func TestR90integCompatImageEditOptionOverridesInPlace(t *testing.T) {
	var mu sync.Mutex
	var fields []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(part)
			if part.FileName() == "" {
				fields = append(fields, part.FormName()+"="+string(b))
			} else {
				fields = append(fields, part.FormName()+"=<file>")
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"AQI="}]}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", Name: "acme", BaseURL: srv.URL, OpenAICompatible: true}).ImageModel("img")
	if err != nil {
		t.Fatal(err)
	}
	n := 1
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat", N: &n, Size: "512x512",
		Files:           []provider.ImageFile{{Type: "file", Data: []byte{1, 2}, MediaType: "image/png"}},
		ProviderOptions: map[string]interface{}{"acme": map[string]interface{}{"size": "1024x1024", "quality": "hd"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"model=img", "prompt=a cat", "image=<file>", "n=1", "size=1024x1024", "quality=hd"}
	if strings.Join(fields, "|") != strings.Join(want, "|") {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
}
