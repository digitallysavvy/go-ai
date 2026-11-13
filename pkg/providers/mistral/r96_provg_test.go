package mistral

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// TestR96provgRealtimeTranscriptionRejectsPrototypeKeys: TS safeParseJSON
// parses server events with secureJsonParse, so an event with a __proto__
// key fails the stream with the JSONParseError.
func TestR96provgRealtimeTranscriptionRejectsPrototypeKeys(t *testing.T) {
	rs := r87provaStartRealtime(t, false, func(send func(string), _ *websocket.Conn) {
		send(`{"type":"transcription.text.delta","text":"x","__proto__":{}}`)
	})
	audio := &r87provaAudio{block: make(chan struct{})}
	res, err := r87provaRealtimeModel(t, rs, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts, err := r87provaDrain(t, res.Stream)
	if err == nil || !strings.Contains(err.Error(), "JSON parsing failed") || !strings.Contains(err.Error(), "Object contains forbidden prototype property") {
		t.Fatalf("err = %v, want the forbidden prototype JSON parse failure", err)
	}
	for _, p := range parts {
		if p.Type == provider.TranscriptionStreamPartTypeDelta {
			t.Fatalf("delta part emitted: %+v", p)
		}
	}
}
