package anthropic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r99provaStreamText streams one call against srv and returns the text and
// the content type of the reported response headers.
func r99provaStreamText(t *testing.T, srv *httptest.Server) (string, string) {
	t.Helper()
	p := New(Config{BaseURL: srv.URL, BearerToken: "t", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text, contentType := "", ""
	if rh, ok := stream.(provider.StreamResponseHeaders); ok {
		for k, v := range rh.ResponseHeaders() {
			if strings.EqualFold(k, "content-type") {
				contentType = v
			}
		}
	}
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		}
	}
	return text, contentType
}

var r99provaEvents = []string{
	`{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":3}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
	`{"type":"message_stop"}`,
}

// TS createAmazonBedrockAnthropicFetch converts the body only when the
// response content type includes application/vnd.amazon.eventstream, and the
// converted response reports content-type text/event-stream; any other
// streaming response goes to the SSE parser as is.
func TestR99provaStreamTransformFollowsContentType(t *testing.T) {
	sse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range r99provaEvents {
			_, _ = io.WriteString(w, "data: "+ev+"\n\n")
		}
	}))
	defer sse.Close()
	if text, ct := r99provaStreamText(t, sse); text != "Hello" || ct != "text/event-stream" {
		t.Fatalf("SSE response: text %q content-type %q, want Hello / text/event-stream", text, ct)
	}

	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		for _, ev := range r99provaEvents {
			_, _ = w.Write(bedrockChunk(t, ev))
		}
	}))
	defer es.Close()
	text, ct := r99provaStreamText(t, es)
	if text != "Hello" || !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("event-stream response: text %q content-type %q, want Hello / text/event-stream", text, ct)
	}
}

// TS convertBase64ToUint8Array (atob) throws for chunk bytes that are not
// base64, and that error fails the stream as is (no Go prefix).
func TestR99provaInvalidChunkBytesErrorAsIs(t *testing.T) {
	frame := encodeEventStreamMessage(t, map[string]string{":message-type": "event", ":event-type": "chunk"}, []byte(`{"bytes":"*"}`))
	_, err := io.ReadAll(transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{}))
	_, want := convertBase64ToBytes("*")
	if err == nil || want == nil || err.Error() != want.Error() {
		t.Fatalf("err = %v, want the base64 decode error %v as is", err, want)
	}
}
