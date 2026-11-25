package anthropic

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
)

// TestR84provaTransformEventStreamTextDecoder: TS
// transformAmazonBedrockEventStreamToSSE parses event.data (TextDecoder text:
// BOM dropped) and decodes the chunk bytes with new TextDecoder() (a
// truncated sequence is one U+FFFD), then re-encodes the text as UTF-8.
func TestR84provaTransformEventStreamTextDecoder(t *testing.T) {
	anthropicEvent := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"a` + "\xE2\x82" + `"}}`
	chunkPayload := "\xEF\xBB\xBF" + `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte("\xEF\xBB\xBF"+anthropicEvent)) + `"}`
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(chunkPayload))
	exception := encodeEventStreamMessage(t, map[string]string{
		":message-type":   "exception",
		":exception-type": "throttlingException",
	}, []byte("slow\xE2\x82down"))
	out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(append(frame, exception...))), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := "data: " + `{"type":"content_block_delta","delta":{"type":"text_delta","text":"a` + "�" + `"}}` + "\n\n" +
		`data: {"error":{"data":"slow` + "�" + `down","isRetryable":true,"message":"slow` + "�" + `down","statusCode":429,"type":"throttlingException"},"type":"error"}` + "\n\n"
	if string(data) != want {
		t.Fatalf("got  %q\nwant %q", string(data), want)
	}
}
