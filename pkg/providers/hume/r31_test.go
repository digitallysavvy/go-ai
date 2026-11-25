package hume

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS JSON.stringify(requestBody) keeps insertion order and HTML characters.
func TestSpeechRequestBodyKeyOrder(t *testing.T) {
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte{1}) })
	model, _ := p.SpeechModel("")
	speed := 1.5
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "a<b", Voice: "v", Speed: &speed, Instructions: "calm",
		ProviderOptions: map[string]interface{}{"hume": SpeechModelOptions{Context: &Context{GenerationID: "g"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"utterances":[{"text":"a<b","speed":1.5,"description":"calm","voice":{"id":"v","provider":"HUME_AI"}}],"format":{"type":"mp3"},"context":{"generation_id":"g"}}`
	if res.Request.Body != want {
		t.Fatalf("request.body = %s\nwant %s", res.Request.Body, want)
	}
}
