package anthropic

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS createEventSourceResponseHandler yields {success:false} for a frame that
// is not JSON and the language model turns it into an `error` part and keeps
// reading; it never ends the stream.
func TestStreamMalformedFrameIsErrorChunkAndStreamContinues(t *testing.T) {
	for _, ev := range []string{"content_block_delta", "content_block_start", "content_block_stop", "message_delta", "message_start"} {
		t.Run(ev, func(t *testing.T) {
			sse := "event: " + ev + "\ndata: {not json\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
			chunk, err := stream.Next()
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if chunk.Type != provider.ChunkTypeError || chunk.Err == nil {
				t.Fatalf("first chunk = %+v, want error chunk", chunk)
			}
			chunk, err = stream.Next()
			if err != nil {
				t.Fatalf("Next() after error chunk: %v", err)
			}
			if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hello" {
				t.Fatalf("second chunk = %+v, want text Hello", chunk)
			}
		})
	}
}
