package googlevertex

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS "should include the request body": request.body is
// JSON.stringify({input, voice, audioConfig}) in that key order; Go marshaled
// a map, which sorts the keys.
func TestR81provaCloudTTSRequestBodyKeyOrder(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()
	model, err := newChirpProvider(t, server.URL).SpeechModel("chirp-3-hd")
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello!"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"input":{"text":"Hello!"},"voice":{"languageCode":"en-US","name":"en-US-Chirp3-HD-Kore"},"audioConfig":{"audioEncoding":"LINEAR16"}}`
	if res.Request == nil || res.Request.Body != want {
		t.Fatalf("request body = %v, want %s", res.Request, want)
	}
}
