package elevenlabs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR89provaCompositionPlanConflictOrder: with both prompt-only options
// next to a composition plan, TS reports forceInstrumental first (TS
// elevenlabs-speech-model.ts getAudioGenerationArgs loops
// ['forceInstrumental', 'musicLengthMs']).
func TestR89provaCompositionPlanConflictOrder(t *testing.T) {
	p, calls := genServer(t)
	_, err := gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{Text: "x", ProviderOptions: el(
		"compositionPlan", m("chunks", []interface{}{}), "musicLengthMs", 5000.0, "forceInstrumental", true)})
	if err == nil || !strings.Contains(err.Error(), "ElevenLabs music forceInstrumental cannot be combined with compositionPlan.") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %d", len(*calls))
	}
}

// TestR89provaKeytermLengthUTF16: z.string().max(20) counts UTF-16 code
// units (TS elevenlabs-transcription-model-options.ts streaming.keyterms),
// so 11 emoji (22 units) are too long.
func TestR89provaKeytermLengthUTF16(t *testing.T) {
	long := strings.Repeat("😀", 11)
	err := validateTranscriptionOptions(map[string]interface{}{"elevenlabs": map[string]interface{}{
		"streaming": map[string]interface{}{"keyterms": []interface{}{long}},
	}})
	if err == nil {
		t.Fatal("expected an error for a 22-unit keyterm")
	}
	if err := validateTranscriptionOptions(map[string]interface{}{"elevenlabs": map[string]interface{}{
		"streaming": map[string]interface{}{"keyterms": []interface{}{strings.Repeat("😀", 10)}},
	}}); err != nil {
		t.Fatalf("20 units: %v", err)
	}
}

// TestR89provaRealtimeErrorEventLoose: TS reads the realtime event without a
// schema and fails with new Error(raw.error ?? fallback), so an object error
// gives "[object Object]" and an empty error stays "" (elevenlabs-
// transcription-model.ts onMessageText). Go dropped the object-error
// message entirely and replaced "" with the fallback.
func TestR89provaRealtimeErrorEventLoose(t *testing.T) {
	for _, tc := range []struct {
		errValue interface{}
		want     string
	}{
		{map[string]interface{}{"code": 1}, "[object Object]"},
		{"", ""},
		{nil, "ElevenLabs realtime transcription error"},
	} {
		server := newRealtimeTestServer(t)
		result, err := newRealtimeTestModel(server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newBlockingAudioStream(),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		})
		if err != nil {
			t.Fatal(err)
		}
		server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "s"}
		server.toSend <- map[string]interface{}{"message_type": "error", "error": tc.errValue}
		done := make(chan error, 1)
		go func() {
			_, err := drainUntilFinishOrError(t, result.Stream)
			done <- err
		}()
		select {
		case err = <-done:
		case <-time.After(5 * time.Second):
			err = errors.New("timed out: the error event was ignored")
		}
		if err == nil || err.Error() != tc.want {
			t.Errorf("error %v: got %v, want %q", tc.errValue, err, tc.want)
		}
		_ = result.Stream.Close()
		server.close()
	}
}
