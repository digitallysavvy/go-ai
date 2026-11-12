package elevenlabs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r91provaSpeech(t *testing.T, modelID string, po map[string]interface{}) r85provaCall {
	t.Helper()
	p, calls := r85provaServer(t, "audio/mpeg", []byte{1, 2, 3})
	opts := &provider.SpeechGenerateOptions{Text: "Hi", Voice: "v", ProviderOptions: po}
	if modelID == SpeechModelMusicV1 {
		opts.Text = "a song"
	}
	if _, err := NewSpeechModel(p, modelID).DoGenerate(t.Context(), opts); err != nil {
		t.Fatalf("%s DoGenerate: %v", modelID, err)
	}
	return (*calls)[0]
}

// prova-03: the speech, dialogue and music queries keep the TS
// queryParams insertion order, output_format then enable_logging
// (elevenlabs-speech-model.ts:178-260, 337-341, 444-448, 555), not sorted.
func TestR91provaSpeechQueryOrder(t *testing.T) {
	po := map[string]interface{}{"elevenlabs": map[string]interface{}{"enableLogging": false}}
	for _, modelID := range []string{SpeechModelMultilingualV2, SpeechModelV4, SpeechModelMusicV1} {
		call := r91provaSpeech(t, modelID, po)
		if call.query != "output_format=mp3_44100_128&enable_logging=false" {
			t.Errorf("%s: query = %q", modelID, call.query)
		}
	}
	if got := speechSearchParams(map[string]string{"z": "1", "enable_logging": "true", "a": "2", "output_format": "pcm_16000"}).Encode(); got != "output_format=pcm_16000&enable_logging=true&a=2&z=1" {
		t.Errorf("speechSearchParams = %q", got)
	}
}

// prova-08 / typed-option ruling: Go-typed literals send the same speech
// request as the map[string]interface{} form (TS elevenlabs-speech-model.ts
// 197-214 reads JSON values); the typed option structs keep working.
func TestR91provaSpeechTypedOptionLiterals(t *testing.T) {
	stability, boost := 0.5, 0.25
	seed := 7.0
	for name, po := range map[string]map[string]interface{}{
		"map[string]float64 voiceSettings": {"elevenlabs": map[string]interface{}{
			"voiceSettings": map[string]float64{"stability": 0.5, "similarityBoost": 0.25}, "seed": int64(7),
			"pronunciationDictionaryLocators": []map[string]interface{}{{"pronunciationDictionaryId": "d", "versionId": "1"}},
		}},
		"plain": {"elevenlabs": map[string]interface{}{
			"voiceSettings": map[string]interface{}{"stability": 0.5, "similarityBoost": 0.25}, "seed": float64(7),
			"pronunciationDictionaryLocators": []interface{}{map[string]interface{}{"pronunciationDictionaryId": "d", "versionId": "1"}},
		}},
		"typed struct": {"elevenlabs": SpeechModelOptions{
			VoiceSettings: &SpeechVoiceSettings{Stability: &stability, SimilarityBoost: &boost}, Seed: &seed,
			PronunciationDictionaryLocators: []PronunciationDictionaryLocator{{PronunciationDictionaryID: "d", VersionID: "1"}},
		}},
		"typed values in a map": {"elevenlabs": map[string]interface{}{
			"voiceSettings": map[string]float64{"stability": 0.5, "similarityBoost": 0.25}, "seed": uint8(7),
			"pronunciationDictionaryLocators": []PronunciationDictionaryLocator{{PronunciationDictionaryID: "d", VersionID: "1"}},
		}},
	} {
		body := r91provaSpeech(t, SpeechModelMultilingualV2, po).body
		vs, _ := body["voice_settings"].(map[string]interface{})
		locators, _ := body["pronunciation_dictionary_locators"].([]interface{})
		if vs["stability"] != 0.5 || vs["similarity_boost"] != 0.25 || body["seed"] != float64(7) || len(locators) != 1 {
			t.Errorf("%s: body = %v", name, body)
		}
	}
}

// Typed-option ruling: a part's newTurn as map[string]bool is read.
func TestR91provaNewTurnTypedLiteral(t *testing.T) {
	got, err := getNewTurn(&provider.SpeechStreamTextPart{ProviderOptions: map[string]interface{}{"elevenlabs": map[string]bool{"newTurn": true}}})
	if err != nil || !got {
		t.Fatalf("getNewTurn = %v, %v", got, err)
	}
}

// prova-08: ElevenLabs transcription options as Go-typed literals
// (map[string]bool streaming, int64 numSpeakers) are validated and sent like
// the map[string]interface{} form.
func TestR91provaTranscriptionTypedOptionLiterals(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"language_code":"eng","language_probability":1,"text":"hi","words":[]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := NewTranscriptionModel(p, ModelScribeV1).DoTranscribe(t.Context(), &provider.TranscriptionOptions{
		Audio: []byte{1, 2, 3}, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{"numSpeakers": int64(2), "tagAudioEvents": false}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if !strings.Contains(form, "name=\"num_speakers\"\r\n\r\n2\r\n") || !strings.Contains(form, "name=\"tag_audio_events\"\r\n\r\nfalse\r\n") {
		t.Errorf("form = %q", form)
	}

	// Streaming: map[string]bool passes validation (the call then fails on
	// the input format, after validation).
	_, err = NewTranscriptionModel(New(Config{APIKey: "k"}), ModelScribeV2Realtime).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(32000)},
		ProviderOptions:  map[string]interface{}{"elevenlabs": map[string]interface{}{"streaming": map[string]bool{"enableLogging": true}}},
	})
	if err == nil || !strings.Contains(err.Error(), "ElevenLabs realtime transcription supports") {
		t.Fatalf("err = %v, want the input format error", err)
	}
}

// Typed-option ruling: numberAsFloat takes every Go integer and float kind.
func TestR91provaNumberAsFloatKinds(t *testing.T) {
	for _, v := range []interface{}{int(3), int8(3), int16(3), int32(3), int64(3), uint(3), uint8(3), uint16(3), uint32(3), uint64(3), float32(3), float64(3)} {
		if f, ok := numberAsFloat(v); !ok || f != 3 {
			t.Errorf("numberAsFloat(%T) = %v, %v", v, f, ok)
		}
	}
}
