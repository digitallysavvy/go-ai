package google

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `if (transcription.languageCode != null) language = ...`: an empty code
// replaces an earlier one (as the Vertex live stream already does).
func TestR83provaLiveTranscriptionEmptyLanguageCodeOverwrites(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if _, err := result.Stream.Next(); err != nil { // stream-start
		t.Fatal(err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "a", "languageCode": "en-US"}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "b", "languageCode": ""}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true, "interactionStatus": "IDLE"}}

	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.Language != "" || finish.FinishText != "ab" {
		t.Fatalf("finish = %+v, TS language \"\" text \"ab\"", finish)
	}
}
