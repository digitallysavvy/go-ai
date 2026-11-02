package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getPcm16SilenceDurationMs returns undefined for audio that does not
// decode, so the part counts as turn activity and resets the trailing
// silence. Go skipped the part before the silence check, so the silence on
// both sides of it added up and finished the stream early.
func TestR81provaSpeechTranslationUndecodableAudioIsActivity(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 500*time.Millisecond)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	audio := func(data string) map[string]interface{} {
		return map[string]interface{}{"serverContent": map[string]interface{}{
			"modelTurn": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"inlineData": map[string]interface{}{"data": data}}}},
		}}
	}
	// 250ms of silence each; the undecodable part in between resets the
	// trailing silence, so only the third silence chunk reaches 500ms.
	silence := pcm16Silence(6000)
	server.toSend <- audio(silence)
	server.toSend <- audio("@@@@")
	server.toSend <- audio(silence)
	server.toSend <- audio(silence)

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	audioParts := 0
	for _, p := range parts {
		if p.Type == provider.SpeechTranslationStreamPartTypeAudio {
			audioParts++
		}
	}
	if audioParts != 3 || parts[len(parts)-1].Type != provider.SpeechTranslationStreamPartTypeFinish {
		t.Fatalf("audio parts before finish = %d, want 3 (parts %+v)", audioParts, parts)
	}
}

// TS request.body is JSON.stringify(requestBody), which keeps the literal's
// key order; Go marshaled maps, which sort keys (parts before role,
// responseFormat first, speaker before style).
func TestR81provaGoogleSpeechRequestBodyKeyOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"AQID"}}]}}]}`))
	}))
	defer server.Close()
	model := NewSpeechModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gemini-3.8-flash-tts-preview")
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hi", Voice: "Kore", Instructions: "calm", OutputFormat: "wav",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"speechMetadata": map[string]interface{}{"speaker": "Joe"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"contents":[{"role":"user","parts":[{"text":"Hi","speechMetadata":{"style":"calm","speaker":"Joe"}}]}],"generationConfig":{"responseModalities":["AUDIO"],"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Kore"}}},"responseFormat":{"audio":{"mimeType":"AUDIO_WAV"}}}}`
	if res.Request == nil || res.Request.Body != want {
		t.Fatalf("request body = %+v\nwant %s", res.Request, want)
	}
}

// TS Math.abs(getInt16(...)) of -32768 is 32768, far above the silence
// threshold; Go negated the int16, which overflows back to -32768, so a
// full-scale negative sample read as silence.
func TestR81provaPCM16SilenceMinSampleIsNotSilence(t *testing.T) {
	if _, silent := pcm16SilenceDurationMs("AIAAgA=="); silent { // two samples of 0x8000
		t.Fatal("-32768 samples were treated as silence")
	}
	// 0x0080 = 128 and 0xff80 = -128 are at the threshold: silence.
	if _, silent := pcm16SilenceDurationMs("gACA/w=="); !silent {
		t.Fatal("samples at the threshold were not silence")
	}
}
