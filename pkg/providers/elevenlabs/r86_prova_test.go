package elevenlabs

import "testing"

// TS elevenlabsFailedResponseHandler parses response.text() (BOM dropped).
func TestR86provaElevenLabsErrorBodyBOM(t *testing.T) {
	if msg, _ := elevenlabsErrorMessage(400, []byte("\xEF\xBB\xBF{\"detail\":\"boom\"}")); msg != "boom" {
		t.Fatalf("message = %q", msg)
	}
}
