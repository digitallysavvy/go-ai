package anthropic

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TS `event.eventType ?? event.exceptionType ?? 'error'`: an exception frame
// that carries an empty :event-type header reports the type "" (not the
// :exception-type), and one without either header reports "error".
func TestR98provaExceptionTypeFromPresentHeaders(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"empty event type", map[string]string{":message-type": "exception", ":event-type": "", ":exception-type": "ThrottlingException"}, ""},
		{"exception type", map[string]string{":message-type": "exception", ":exception-type": "ThrottlingException"}, "ThrottlingException"},
		{"empty exception type", map[string]string{":message-type": "exception", ":exception-type": ""}, ""},
		{"no type header", map[string]string{":message-type": "exception"}, "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := encodeEventStreamMessage(t, tc.headers, []byte(`{"message":"m"}`))
			out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
			data, err := io.ReadAll(out)
			if err != nil {
				t.Fatal(err)
			}
			var frameOut struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(string(data), "data: "), "\n\n")), &frameOut); err != nil {
				t.Fatalf("output %q: %v", data, err)
			}
			if frameOut.Error.Type != tc.want {
				t.Fatalf("error.type = %q, want %q (output %q)", frameOut.Error.Type, tc.want, data)
			}
		})
	}
}
