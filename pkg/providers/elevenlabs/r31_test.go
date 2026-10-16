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

// TS mediaTypeToExtension reads the second "/" segment only.
func TestAudioExtensionMatchesTS(t *testing.T) {
	for in, want := range map[string]string{"audio/mpeg": "mp3", "audio/x-wav": "wav", "audio/a/b": "a", "audio/mp4": "m4a", "": ""} {
		if got := elevenLabsAudioExtension(in); got != want {
			t.Errorf("elevenLabsAudioExtension(%q) = %q, want %q", in, got, want)
		}
	}
}

// zod `.nullish().default(x)` defaults only a missing key; an explicit null
// stays null and TS skips the field.
func TestTranscriptionExplicitNullOptionsSkipDefaults(t *testing.T) {
	capture := map[string]string{}
	srv := transcriptionTestServer(t, &capture)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel(ModelScribeV1)
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{
			"tagAudioEvents": nil, "timestampsGranularity": nil, "fileFormat": nil, "diarize": nil,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"tag_audio_events", "timestamps_granularity", "file_format"} {
		if v, ok := capture[k]; ok {
			t.Errorf("%s = %q was sent for an explicit null", k, v)
		}
	}
	if capture["diarize"] != "true" {
		t.Errorf("diarize = %q, want true (?? true)", capture["diarize"])
	}
	// A missing key still gets the zod default.
	capture = map[string]string{}
	_, _ = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{}},
	})
	if capture["tag_audio_events"] != "true" || capture["timestamps_granularity"] != "word" || capture["file_format"] != "other" {
		t.Errorf("defaults missing: %v", capture)
	}
}
