package google

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `new Error(message.error.message ?? 'Google Live API error')`: an empty
// server error message is kept, only an absent one takes the default.
func TestR84provaLiveEmptyErrorMessageKept(t *testing.T) {
	for _, msg := range []map[string]interface{}{{"message": ""}, {}} {
		want := "Google Live API error"
		if _, ok := msg["message"]; ok {
			want = ""
		}
		t.Run("transcription", func(t *testing.T) {
			server := newLiveTranscriptionTestServer(t)
			defer server.close()
			model := newLiveTestModel(t, server.ts.URL)
			result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio:            newChanAudioStream([]byte{1}),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Stream.Close() //nolint:errcheck
			server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
			server.toSend <- map[string]interface{}{"error": msg}
			_, err = drainUntilFinishOrError(t, result.Stream)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
		t.Run("translation", func(t *testing.T) {
			server := newLiveTranscriptionTestServer(t)
			defer server.close()
			model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
			result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
				Audio:            newChanAudioStream([]byte{1}),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
				TargetLanguage:   "es",
			})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Stream.Close() //nolint:errcheck
			server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
			server.toSend <- map[string]interface{}{"error": msg}
			_, err = drainUntilTranslationFinishOrError(t, result.Stream)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}
