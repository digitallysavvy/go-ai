package hume

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS maps context.utterances even when the array is empty: `context:
// {utterances: []}` is sent.
func TestSpeechEmptyContextUtterancesAreSent(t *testing.T) {
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte{1}) })
	model, _ := p.SpeechModel("")
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "hi", Voice: "v",
		ProviderOptions: map[string]interface{}{"hume": map[string]interface{}{"context": map[string]interface{}{"utterances": []interface{}{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"utterances":[{"text":"hi","voice":{"id":"v","provider":"HUME_AI"}}],"format":{"type":"mp3"},"context":{"utterances":[]}}`
	if res.Request.Body != want {
		t.Fatalf("request.body = %s\nwant %s", res.Request.Body, want)
	}
}
