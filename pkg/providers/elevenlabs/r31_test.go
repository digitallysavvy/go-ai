package elevenlabs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS JSON.stringify(requestBody) keeps insertion order (voice_settings last)
// and leaves HTML characters literal.
func TestSpeechRequestBodyKeyOrder(t *testing.T) {
	p := newElevenLabsProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("a"))}, nil
	})
	speed, stab, seed := 1.1, 0.4, 7.0
	res, err := NewSpeechModel(p, "eleven_v3").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "a<b", Speed: &speed, Language: "en",
		ProviderOptions: map[string]interface{}{"elevenlabs": SpeechModelOptions{
			Seed: &seed, PreviousText: "p", VoiceSettings: &SpeechVoiceSettings{Stability: &stab},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"a<b","model_id":"eleven_v3","language_code":"en","seed":7,"previous_text":"p","voice_settings":{"speed":1.1,"stability":0.4}}`
	if res.Request.Body != want {
		t.Fatalf("request.body = %s\nwant %s", res.Request.Body, want)
	}
}
