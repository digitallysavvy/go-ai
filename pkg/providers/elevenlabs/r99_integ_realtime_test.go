package elevenlabs

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS elevenlabs-transcription-model.ts: a committed transcript's text goes
// through `(raw.text ?? '').trim()` and the timestamped words through
// `words.filter(word => typeof word.start ...)`. A non-string text, a
// non-array words value or a null word throws a TypeError, which fails the
// stream (onProcessingError). Go read them as empty and went on.
func TestR99integRealtimeTypeErrorsFailStream(t *testing.T) {
	for name, tc := range map[string]struct {
		msg  map[string]interface{}
		want string
	}{
		"committed text number":       {map[string]interface{}{"message_type": "committed_transcript", "text": 5}, `(raw.text ?? "").trim is not a function`},
		"final text object":           {map[string]interface{}{"message_type": "final_transcript", "text": map[string]interface{}{}}, `(raw.text ?? "").trim is not a function`},
		"timestamps text bool":        {map[string]interface{}{"message_type": "committed_transcript_with_timestamps", "text": true}, `(raw.text ?? "").trim is not a function`},
		"timestamps words string":     {map[string]interface{}{"message_type": "committed_transcript_with_timestamps", "text": "hi", "words": "ab"}, "words.filter is not a function"},
		"timestamps words null entry": {map[string]interface{}{"message_type": "final_transcript_with_timestamps", "text": "hi", "words": []interface{}{nil}}, "Cannot read properties of null (reading 'start')"},
	} {
		t.Run(name, func(t *testing.T) {
			server := newRealtimeTestServer(t)
			defer server.close()
			result, err := newRealtimeTestModel(server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio:            newBlockingAudioStream(),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Stream.Close() //nolint:errcheck
			server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "s"}
			server.toSend <- tc.msg
			_, err = drainUntilFinishOrError(t, result.Stream)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
