package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ports of the TS google-speech-model tests for Gemini 3.8 structured speech.

func structuredSpeechServer(t *testing.T, mime string, audio []byte, body *map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"` + mime + `","data":"` + base64.StdEncoding.EncodeToString(audio) + `"}}]}}]}`))
	}))
}

func speechParts(body map[string]interface{}) []interface{} {
	return body["contents"].([]interface{})[0].(map[string]interface{})["parts"].([]interface{})
}

func TestStructuredSpeechTurnsStylesAndResponseFormat(t *testing.T) {
	var body map[string]interface{}
	wav := []byte("RIFF....WAVEfmt ")
	srv := structuredSpeechServer(t, "audio/wav", wav, &body)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(ModelGemini38FlashTTS)

	style := "whisper"
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "",
		Instructions: "Warm and friendly",
		OutputFormat: "audio/wav",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"multiSpeakerVoiceConfig": map[string]interface{}{"speakerVoiceConfigs": []interface{}{
				map[string]interface{}{"speaker": "Ann", "voiceConfig": map[string]interface{}{"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Kore"}}},
				map[string]interface{}{"speaker": "Bob", "voiceConfig": map[string]interface{}{"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Puck"}}},
			}},
			"turns": []interface{}{
				map[string]interface{}{"text": "Hi Bob.", "speechMetadata": map[string]interface{}{"speaker": "Ann"}},
				map[string]interface{}{"text": "Hi Ann.", "speechMetadata": map[string]interface{}{"speaker": "Bob", "style": style}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := speechParts(body)
	if len(parts) != 2 {
		t.Fatalf("parts = %#v", parts)
	}
	p0 := parts[0].(map[string]interface{})
	p1 := parts[1].(map[string]interface{})
	// Instructions become each turn's style unless the turn sets its own.
	if p0["text"] != "Hi Bob." || p0["speechMetadata"].(map[string]interface{})["style"] != "Warm and friendly" || p0["speechMetadata"].(map[string]interface{})["speaker"] != "Ann" {
		t.Errorf("part 0 = %#v", p0)
	}
	if p1["speechMetadata"].(map[string]interface{})["style"] != "whisper" {
		t.Errorf("part 1 = %#v", p1)
	}
	gen := body["generationConfig"].(map[string]interface{})
	if gen["responseFormat"].(map[string]interface{})["audio"].(map[string]interface{})["mimeType"] != "AUDIO_WAV" {
		t.Errorf("responseFormat = %#v", gen["responseFormat"])
	}
	// Gemini 3.8 already returns a WAV container: no second header.
	if string(res.Audio) != string(wav) {
		t.Errorf("audio was re-wrapped: %q", res.Audio)
	}
	for _, w := range res.Warnings {
		t.Errorf("unexpected warning %+v", w)
	}
}

func TestStructuredSpeechPlainTextAndFormats(t *testing.T) {
	var body map[string]interface{}
	srv := structuredSpeechServer(t, "audio/L16;rate=24000", []byte{1, 0, 2, 0}, &body)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(ModelGemini38FlashTTS)

	// No outputFormat: no responseFormat is sent; PCM replies are wrapped as WAV.
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["generationConfig"].(map[string]interface{})["responseFormat"]; ok {
		t.Error("responseFormat sent without an outputFormat")
	}
	if len(res.Audio) != 44+4 || string(res.Audio[:4]) != "RIFF" {
		t.Errorf("audio len=%d", len(res.Audio))
	}
	if parts := speechParts(body); parts[0].(map[string]interface{})["speechMetadata"] != nil {
		t.Errorf("part = %#v (no metadata expected)", parts[0])
	}

	for in, want := range map[string]string{"mulaw": "AUDIO_MULAW", "audio/alaw": "AUDIO_ALAW", "pcm": "AUDIO_L16", "audio/l16": "AUDIO_L16"} {
		_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello", OutputFormat: in})
		if err != nil {
			t.Fatal(err)
		}
		got := body["generationConfig"].(map[string]interface{})["responseFormat"].(map[string]interface{})["audio"].(map[string]interface{})["mimeType"]
		if got != want {
			t.Errorf("%s -> %v, want %s", in, got, want)
		}
	}
	res, err = m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello", OutputFormat: "mp3"})
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0].Details, "Unsupported output format: mp3. Using wav instead.") {
		t.Fatalf("warnings = %+v err=%v", res, err)
	}
}

func TestStructuredSpeechValidation(t *testing.T) {
	var body map[string]interface{}
	srv := structuredSpeechServer(t, "audio/wav", []byte("x"), &body)
	defer srv.Close()
	structured, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(ModelGemini38FlashTTS)
	legacy, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(ModelGemini25FlashTTS)

	turn := func(text string) map[string]interface{} { return map[string]interface{}{"text": text} }
	cases := []struct {
		name    string
		model   provider.SpeechModel
		opts    provider.SpeechGenerateOptions
		field   string
		message string
	}{
		{"custom voice", structured, provider.SpeechGenerateOptions{Text: "hi", Voice: "voice_abc"}, "voice", "Custom voices are not supported. Use a prebuilt voice instead."},
		{"empty transcript", structured, provider.SpeechGenerateOptions{Text: ""}, "text", "Speech input must contain a non-empty transcript."},
		{"turns and speechMetadata", structured, provider.SpeechGenerateOptions{ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"turns": []interface{}{turn("a")}, "speechMetadata": map[string]interface{}{"style": "x"}}}}, "providerOptions", "Set speechMetadata on each turn when using turns."},
		{"legacy model with turns", legacy, provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"turns": []interface{}{turn("a")}}}}, "providerOptions", "Structured speech metadata and turns require Gemini 3.8 TTS."},
		{"speaker not configured", structured, provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"multiSpeakerVoiceConfig": map[string]interface{}{"speakerVoiceConfigs": []interface{}{
				map[string]interface{}{"speaker": "Ann", "voiceConfig": map[string]interface{}{"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Kore"}}}}}}}},
			"speechMetadata.speaker", "Every multi-speaker turn must specify a speechMetadata.speaker matching a configured speaker."},
	}
	for _, tc := range cases {
		_, err := tc.model.DoGenerate(context.Background(), &tc.opts)
		var iae *providererrors.InvalidArgumentError
		if !errors.As(err, &iae) || iae.Field != tc.field || iae.Message != tc.message {
			t.Errorf("%s: err = %#v", tc.name, err)
		}
	}

	// Turns replace the top-level text with a warning.
	res, err := structured.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "ignored", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"turns": []interface{}{turn("real")}}}})
	if err != nil || len(res.Warnings) != 1 || res.Warnings[0].Details != "Google TTS turns replace the top-level text." {
		t.Fatalf("warnings = %+v err=%v", res, err)
	}
	if speechParts(body)[0].(map[string]interface{})["text"] != "real" {
		t.Errorf("parts = %#v", speechParts(body))
	}
}

// Legacy (2.5 / 3.1) models keep prompt-prefixed directions and always wrap
// raw PCM replies, whatever the (ignored) mime type says.
func TestLegacySpeechInstructionsAndWrapping(t *testing.T) {
	var body map[string]interface{}
	srv := structuredSpeechServer(t, "audio/L16;rate=16000", []byte{1, 0}, &body)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(ModelGemini31FlashTTSPreview)
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello", Instructions: "Cheerfully"})
	if err != nil {
		t.Fatal(err)
	}
	if speechParts(body)[0].(map[string]interface{})["text"] != "Cheerfully: hello" {
		t.Errorf("parts = %#v", speechParts(body))
	}
	if _, ok := body["generationConfig"].(map[string]interface{})["responseFormat"]; ok {
		t.Error("legacy models never send responseFormat")
	}
	if string(res.Audio[:4]) != "RIFF" {
		t.Error("PCM reply must be wrapped as WAV")
	}
}
