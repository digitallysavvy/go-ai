package elevenlabs

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS elevenlabsFailedResponseHandler parses response.text() (BOM dropped).
func TestR86provaElevenLabsErrorBodyBOM(t *testing.T) {
	if msg, _ := elevenlabsErrorMessage(400, []byte("\xEF\xBB\xBF{\"detail\":\"boom\"}")); msg != "boom" {
		t.Fatalf("message = %q", msg)
	}
}

// TS resolves the apiKey function for the WebSocket streams too (TS
// 7716e189ee, config.headers()): Go streams read only APIKey and
// ELEVENLABS_API_KEY, so an APIKeyFunc provider failed (or sent the
// environment key).
func TestR86provaElevenLabsStreamsUseAPIKeyFunc(t *testing.T) {
	t.Setenv("ELEVENLABS_API_KEY", "")
	keyFunc := func(context.Context) (string, error) { return "func-key", nil }

	server := newRealtimeTestServer(t)
	defer server.close()
	model := NewTranscriptionModel(New(Config{BaseURL: server.ts.URL, APIKeyFunc: keyFunc}), ModelScribeV2Realtime)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("transcription DoStream: %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "s"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatal(err)
	}
	if _, apiKey := server.handshakeHeaders(); apiKey != "func-key" {
		t.Fatalf("transcription handshake xi-api-key = %q", apiKey)
	}

	speech := NewSpeechModel(New(Config{APIKeyFunc: keyFunc}), "eleven_flash_v2_5")
	_, apiKey, err := speech.streamHeaders(context.Background(), nil)
	if err != nil || apiKey != "func-key" {
		t.Fatalf("speech stream key = %q, err = %v", apiKey, err)
	}
}
