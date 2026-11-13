package deepgram

import (
	"testing"
)

// TS createJsonResponseHandler: JSON.parse reads 1e999 as Infinity and zod
// strips the undeclared key (ruling 4).
func TestR98provcDeepgramResponseOutOfRangeUndeclared(t *testing.T) {
	body := []byte(`{"metadata":{"duration":1.5,"sha256":1e999},"results":{"channels":[{"alternatives":[{"transcript":"hi","confidence":1e999,"words":[]}]}]}}`)
	if err := validateTranscriptionResponse(200, nil, body); err != nil {
		t.Fatalf("want valid, got %v", err)
	}
}
