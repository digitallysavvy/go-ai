package fishaudio

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS JSON.stringify(requestBody) keeps insertion order and HTML characters.
func TestSpeechRequestBodyKeyOrder(t *testing.T) {
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte{1}) })
	model := mustSpeechModel(t, p, "s2-pro")
	speed, volume, temp := 1.5, 2.0, 0.5
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "a<b", Voice: "v", Speed: &speed,
		ProviderOptions: map[string]interface{}{"fishAudio": SpeechModelOptions{Volume: &volume, Temperature: &temp}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"a<b","format":"mp3","reference_id":"v","prosody":{"speed":1.5,"volume":2},"temperature":0.5}`
	if res.Request.Body != want {
		t.Fatalf("request.body = %s\nwant %s", res.Request.Body, want)
	}
}

func TestTranscriptionFilePartWithoutMediaTypeIsOctetStream(t *testing.T) {
	m := &TranscriptionModel{modelID: ModelTranscribe1}
	body, contentType, err := m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte("a")})
	if err != nil || contentType == "" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(body.String(), "Content-Type: application/octet-stream") {
		t.Fatalf("body = %q", body.String())
	}
}

func TestBaseURLTrailingSlashIsStripped(t *testing.T) {
	var path string
	p, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path; _, _ = w.Write([]byte{1}) })
	p2 := New(Config{APIKey: "k", BaseURL: srv.URL + "/"})
	_ = p
	if _, err := mustSpeechModel(t, p2, "s2-pro").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/tts" {
		t.Fatalf("path = %q", path)
	}
}
