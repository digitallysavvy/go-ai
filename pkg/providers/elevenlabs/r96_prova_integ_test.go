package elevenlabs

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ruling 4 (r96): a number beyond the double range reads as +Inf, as
// JSON.parse does, in the raw chunk of the realtime transcription stream.
func TestR96provaTranscriptionRawChunkOutOfRange(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()
	result, err := newRealtimeTestModel(server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newBlockingAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.toSend <- json.RawMessage(`{"message_type":"session_started","session_id":"s","x":1e400}`)
	timer := time.AfterFunc(5*time.Second, func() { _ = result.Stream.Close() })
	defer timer.Stop()
	for {
		part, err := result.Stream.Next()
		if err != nil {
			t.Fatalf("no raw part: %v", err)
		}
		if part.Type != provider.TranscriptionStreamPartTypeRaw {
			continue
		}
		raw, _ := part.RawValue.(map[string]interface{})
		if x, _ := raw["x"].(float64); !math.IsInf(x, 1) {
			t.Fatalf("raw = %#v, want x = +Inf", part.RawValue)
		}
		return
	}
}

// Ruling 4 (r96): a non-string error holding 1e400 is JSON.stringify of
// Infinity: null.
func TestR96provaSpeechErrorOutOfRange(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi")})
	server.waitForCount(t, 3, 2*time.Second)
	server.toSend <- json.RawMessage(`{"error":{"code":1e400}}`)
	timer := time.AfterFunc(5*time.Second, func() { _ = result.Stream.Close() })
	defer timer.Stop()
	_, err := collectSpeech(t, result.Stream)
	if err == nil || err.Error() != `ElevenLabs speech stream error: {"code":null}` {
		t.Fatalf("err = %v", err)
	}
}

// Ruling 4 (r96): a non-finite number in a message the SDK sends is written
// as null (JSON.stringify), not a marshal failure.
func TestR96provaSpeechErrorTextNonFinite(t *testing.T) {
	text, ok := errorText(map[string]interface{}{"error": map[string]interface{}{"code": math.Inf(1)}})
	if !ok || text != `{"code":null}` {
		t.Fatalf("errorText = %q, %v", text, ok)
	}
}
