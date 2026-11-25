package fishaudio

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcForgivingBase64 (r92 provc-04): AudioBase64 decodes like TS
// convertBase64ToUint8Array (ASCII whitespace ignored, padding optional).
func TestR92provcForgivingBase64(t *testing.T) {
	tm := &TranscriptionModel{provider: New(Config{APIKey: "k"}), modelID: "s1"}
	body, _, err := tm.buildMultipartBody(&provider.TranscriptionOptions{AudioBase64: "aG\n k", MimeType: "audio/wav"})
	if err != nil || !strings.Contains(body.String(), "\r\n\r\nhi\r\n") {
		t.Fatalf("err=%v body=%q", err, body)
	}
}
