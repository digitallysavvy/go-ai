package google

import (
	"context"
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
