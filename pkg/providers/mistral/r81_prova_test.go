package mistral

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// TS formatFileUrl embeds convertToBase64(part.data.data), which returns a
// base64 string payload as is; Go read only the bytes, so a DataString part
// became an empty data URL (and a bare image type could not be sniffed).
func TestR81provaMistralFileDataStringPayload(t *testing.T) {
	for _, tc := range []struct{ mediaType, data, want string }{
		{"image/png", "-_8", "data:image/png;base64,-_8"},
		{"image", "iVBORw", "data:image/png;base64,iVBORw"},
	} {
		got, err := convertMistralFilePart(types.FileContent{MediaType: tc.mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: tc.data}})
		if err != nil {
			t.Fatalf("%s: %v", tc.mediaType, err)
		}
		if got["image_url"] != tc.want {
			t.Fatalf("%s: part = %v, want image_url %s", tc.mediaType, got, tc.want)
		}
	}
}

// TS "should redact reference audio": request.body is
// JSON.stringify({model, input, ref_audio: '[redacted]', response_format,
// stream}) in the literal's key order; Go marshaled a map (sorted keys).
func TestR81provaMistralSpeechRequestBodyKeyOrder(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(*http.Request) (*http.Response, error) {
		return mistralJSONResponse(`{"audio_data":"AQID"}`), nil
	})
	res, err := NewSpeechModel(p, "voxtral-mini-tts-latest").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "Hello from the AI SDK!",
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"refAudio": "sensitive-reference-audio"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model":"voxtral-mini-tts-latest","input":"Hello from the AI SDK!","ref_audio":"[redacted]","response_format":"mp3","stream":false}`
	if res.Request == nil || res.Request.Body != want {
		t.Fatalf("request body = %+v, want %s", res.Request, want)
	}
}
