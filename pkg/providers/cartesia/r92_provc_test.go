package cartesia

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r92provcSpacedBase64 is "hi" in base64 with ASCII whitespace and no
// padding: TS convertBase64ToUint8Array (atob's forgiving decode) accepts it.
const r92provcSpacedBase64 = "aG\n k"

// TestR92provcForgivingBase64 (r92 provc-04): realtime input audio and
// AudioBase64 transcription audio decode like TS convertBase64ToUint8Array.
func TestR92provcForgivingBase64(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	audio, binary, _, err := m.SerializeClientEventRaw(provider.RealtimeClientEvent{Type: "input-audio-append", Audio: r92provcSpacedBase64})
	if err != nil || !binary || string(audio) != "hi" {
		t.Fatalf("realtime audio = %q binary=%v err=%v", audio, binary, err)
	}
	tm := &TranscriptionModel{provider: New(Config{APIKey: "k"}), modelID: "ink-whisper"}
	body, _, _, err := tm.buildMultipartBody(&provider.TranscriptionOptions{AudioBase64: r92provcSpacedBase64, MimeType: "audio/wav"})
	if err != nil || !strings.Contains(body.String(), "\r\n\r\nhi\r\n") {
		t.Fatalf("transcription body err=%v body=%q", err, body)
	}
}
