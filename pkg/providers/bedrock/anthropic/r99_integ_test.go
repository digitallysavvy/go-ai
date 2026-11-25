package anthropic

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// TS amazon-bedrock-anthropic-fetch.ts convertBase64ToUint8Array: the stream
// fails with atob's DOMException message as is (cr-r99-prova-to-integrator).
func TestR99integInvalidChunkBytesAtobText(t *testing.T) {
	for in, want := range map[string]string{
		"*":     "Invalid character",
		"AAAAA": "The string to be decoded is not correctly encoded.",
	} {
		frame := encodeEventStreamMessage(t, map[string]string{":message-type": "event", ":event-type": "chunk"}, []byte(`{"bytes":"`+in+`"}`))
		_, err := io.ReadAll(transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{}))
		if err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", in, err, want)
		}
	}
}
