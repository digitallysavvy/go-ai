package fishaudio

import (
	"context"
	"net/http"
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
