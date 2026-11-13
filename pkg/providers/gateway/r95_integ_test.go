package gateway

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/speechstream"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// TS parseSpeechStreamPart keeps an audio part whatever its base64 holds;
// streamSpeech's convertBase64ToUint8Array then throws atob's
// InvalidCharacterError, which errors the stream as is (no asGatewayError)
// after the earlier parts were delivered, and cancels the socket and text.
func TestR95integSpeechStreamUndecodableAudioFails(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	text := newSpeechTestText()
	text.block = true
	res, err := newTestGatewaySpeechModel(t, server.ts.URL).DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.start", 2*time.Second)
	server.toSend <- map[string]interface{}{"type": "stream-start", "warnings": []interface{}{}}
	server.toSend <- map[string]interface{}{"type": "audio", "audio": "AAAA", "audioFormat": map[string]interface{}{"type": "audio/pcm"}}
	server.toSend <- map[string]interface{}{"type": "audio", "audio": "@@@@", "audioFormat": map[string]interface{}{"type": "audio/pcm"}}

	var types []string
	var audio [][]byte
	got := r95provgDrain(t, func() error {
		part, err := res.Stream.Next()
		if err == nil {
			types = append(types, part.Type)
			if part.Type == provider.SpeechStreamPartTypeAudio {
				audio = append(audio, part.Audio)
			}
		}
		return err
	})
	if !reflect.DeepEqual(types, []string{"stream-start", "audio"}) || !reflect.DeepEqual(audio, [][]byte{{0, 0, 0}}) {
		t.Fatalf("parts = %v audio = %v, want stream-start then the first audio part", types, audio)
	}
	var ice *speechstream.InvalidCharacterError
	if !errors.As(got, &ice) || got.Error() != "Invalid character" {
		t.Fatalf("err = %T %v, want *speechstream.InvalidCharacterError \"Invalid character\"", got, got)
	}
	if gatewayerrors.IsGatewayError(got) {
		t.Fatalf("err = %T, want the decode error itself, not a GatewayError", got)
	}
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text stream was not cancelled after the decode failure")
	}
}
