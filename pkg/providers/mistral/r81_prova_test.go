package mistral

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS speech returns `audio: response.audio_data` (a base64 string) and core
// decodes it with convertBase64ToUint8Array, which accepts base64url and
// unpadded input; Go decoded with base64.StdEncoding and failed.
func TestR81provaMistralSpeechForgivingBase64(t *testing.T) {
	// 0xfb 0xff -> standard "+/8=", base64url unpadded "-_8".
	p := newMistralProviderWithTransport(t, func(*http.Request) (*http.Response, error) {
		return mistralJSONResponse(`{"audio_data":"-_8"}`), nil
	})
	res, err := NewSpeechModel(p, "voxtral-mini-tts-latest").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "v"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if string(res.Audio) != "\xfb\xff" {
		t.Fatalf("audio = %x, want fbff", res.Audio)
	}
}
