package elevenlabs

import (
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS core streamSpeech decodes an audio part with convertBase64ToUint8Array;
// atob's error for audio that is not base64 fails the stream as is (no Go
// prefix).
func TestR99provaInvalidStreamAudioErrorAsIs(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi ")})
	server.waitForCount(t, 1, 2*time.Second)
	server.toSend <- m("audio", "*")
	_, err := collectSpeech(t, result.Stream)
	_, want := types.DecodeFileDataString("*")
	if err == nil || want == nil || err.Error() != want.Error() {
		t.Fatalf("err = %v, want the base64 decode error %v as is", err, want)
	}
}
